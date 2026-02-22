import ArgumentParser
import CryptoKit
import Foundation
import Network

// MARK: - Daemon Protocol
// Simple line-based JSON protocol over Unix socket
// Request: {"cmd": "jwt_sign", "issuer_id": "...", "key_id": "...", "key_path": "..."}
// Response: {"success": true, "token": "...", "expires_in": 600}

// MARK: - Cached State

class DaemonState {
    static let shared = DaemonState()
    
    // Cache private keys in memory (key_path -> PrivateKey)
    var keyCache: [String: P256.Signing.PrivateKey] = [:]
    private let cacheLock = NSLock()
    
    func getKey(path: String) throws -> P256.Signing.PrivateKey {
        cacheLock.lock()
        if let cached = keyCache[path] {
            cacheLock.unlock()
            return cached
        }
        cacheLock.unlock()
        
        // Load and cache
        let key = try loadPrivateKey(from: path)
        cacheLock.lock()
        keyCache[path] = key
        cacheLock.unlock()
        return key
    }
    
    func clearCache() {
        cacheLock.lock()
        keyCache.removeAll()
        cacheLock.unlock()
    }
}

// MARK: - JWT Functions (from asc-jwt-sign)

let jwtTokenLifetime: TimeInterval = 10 * 60

func base64URLEncode(_ data: Data) -> String {
    data.base64EncodedString()
        .replacingOccurrences(of: "+", with: "-")
        .replacingOccurrences(of: "/", with: "_")
        .replacingOccurrences(of: "=", with: "")
}

struct JWTHeader: Encodable {
    let alg: String = "ES256"
    let kid: String
    let typ: String = "JWT"
}

struct JWTClaims: Encodable {
    let iss: String
    let iat: Int
    let exp: Int
    let aud: String
}

func generateJWT(issuerID: String, keyID: String, privateKey: P256.Signing.PrivateKey) throws -> String {
    let now = Date()
    let iat = Int(now.timeIntervalSince1970)
    let exp = Int(now.addingTimeInterval(jwtTokenLifetime).timeIntervalSince1970)
    
    let header = JWTHeader(kid: keyID)
    let claims = JWTClaims(
        iss: issuerID,
        iat: iat,
        exp: exp,
        aud: "appstoreconnect-v1"
    )
    
    let encoder = JSONEncoder()
    encoder.outputFormatting = .sortedKeys
    
    let headerData = try encoder.encode(header)
    let payloadData = try encoder.encode(claims)
    
    let headerEncoded = base64URLEncode(headerData)
    let payloadEncoded = base64URLEncode(payloadData)
    let signingInput = "\(headerEncoded).\(payloadEncoded)"
    
    guard let data = signingInput.data(using: .utf8) else {
        throw DaemonError.signingFailed("Failed to encode signing input")
    }
    
    let signature = try privateKey.signature(for: data)
    let signatureEncoded = base64URLEncode(signature.rawRepresentation)
    
    return "\(signingInput).\(signatureEncoded)"
}

enum DaemonError: Error {
    case signingFailed(String)
    case invalidPrivateKey(String)
    case keyFileReadError(String)
}

func loadPrivateKey(from path: String) throws -> P256.Signing.PrivateKey {
    let url = URL(fileURLWithPath: path)
    let pemData = try Data(contentsOf: url)
    
    guard let pemString = String(data: pemData, encoding: .utf8) else {
        throw DaemonError.keyFileReadError("File is not valid UTF-8")
    }
    
    let lines = pemString.components(separatedBy: .newlines)
    let base64Lines = lines.filter { !$0.hasPrefix("-") && !$0.isEmpty }
    let base64String = base64Lines.joined()
    
    guard let keyData = Data(base64Encoded: base64String) else {
        throw DaemonError.invalidPrivateKey("Failed to decode base64 content")
    }
    
    // Try SEC1 format first
    if keyData.count == 32 {
        do {
            return try P256.Signing.PrivateKey(rawRepresentation: keyData)
        } catch {
            // Fall through
        }
    }
    
    // Try PKCS#8
    let privateKeyBytes = try extractSEC1FromPKCS8(keyData)
    return try P256.Signing.PrivateKey(rawRepresentation: privateKeyBytes)
}

func extractSEC1FromPKCS8(_ data: Data) throws -> Data {
    var index = 0
    
    func readLength(from data: Data, at index: inout Int) throws -> Int {
        guard index < data.count else {
            throw DaemonError.invalidPrivateKey("Unexpected end of data")
        }
        let byte = data[index]
        if byte & 0x80 == 0 {
            index += 1
            return Int(byte)
        } else {
            let numBytes = Int(byte & 0x7F)
            index += 1
            var length = 0
            for _ in 0..<numBytes {
                guard index < data.count else {
                    throw DaemonError.invalidPrivateKey("Unexpected end of data")
                }
                length = (length << 8) + Int(data[index])
                index += 1
            }
            return length
        }
    }
    
    func skipElement(from data: Data, at index: inout Int) throws {
        guard index < data.count else { return }
        _ = data[index]
        index += 1
        let length = try readLength(from: data, at: &index)
        index += length
    }
    
    guard index < data.count && data[index] == 0x30 else {
        throw DaemonError.invalidPrivateKey("Expected SEQUENCE")
    }
    index += 1
    _ = try readLength(from: data, at: &index)
    
    try skipElement(from: data, at: &index)
    try skipElement(from: data, at: &index)
    
    guard index < data.count && data[index] == 0x04 else {
        throw DaemonError.invalidPrivateKey("Expected OCTET STRING")
    }
    index += 1
    let privateKeyLength = try readLength(from: data, at: &index)
    
    let ecPrivateKeyData = data.subdata(in: index..<(index + privateKeyLength))
    
    var ecIndex = 0
    guard ecIndex < ecPrivateKeyData.count && ecPrivateKeyData[ecIndex] == 0x30 else {
        throw DaemonError.invalidPrivateKey("Expected SEQUENCE for ECPrivateKey")
    }
    ecIndex += 1
    _ = try readLength(from: ecPrivateKeyData, at: &ecIndex)
    
    try skipElement(from: ecPrivateKeyData, at: &ecIndex)
    
    guard ecIndex < ecPrivateKeyData.count && ecPrivateKeyData[ecIndex] == 0x04 else {
        throw DaemonError.invalidPrivateKey("Expected OCTET STRING for EC privateKey")
    }
    ecIndex += 1
    let sec1KeyLength = try readLength(from: ecPrivateKeyData, at: &ecIndex)
    
    return ecPrivateKeyData.subdata(in: ecIndex..<(ecIndex + sec1KeyLength))
}

// MARK: - Daemon Server

class DaemonServer {
    let socketPath: String
    var listener: NWListener?
    
    init(socketPath: String) {
        self.socketPath = socketPath
    }
    
    func start() throws {
        // Remove existing socket file
        let fm = FileManager.default
        if fm.fileExists(atPath: socketPath) {
            try fm.removeItem(atPath: socketPath)
        }
        
        // Create NWListener with Unix socket
        let params = NWParameters.tcp
        params.allowLocalEndpointReuse = true
        
        do {
            guard let port = NWEndpoint.Port(rawValue: 0) else {
                throw DaemonError.signingFailed("Invalid port")
            }
            listener = try NWListener(using: params, on: port)
        } catch {
            // Fallback: use direct socket
            print("Warning: NWListener not available, falling back to direct socket")
            try startDirectSocket()
            return
        }
        
        guard let listener = listener else {
            throw DaemonError.signingFailed("Failed to create listener")
        }
        
        listener.newConnectionHandler = { [weak self] connection in
            guard let self = self else { return }
            self.handleConnection(connection)
        }
        
        let socketPathForHandler = socketPath
        listener.stateUpdateHandler = { state in
            switch state {
            case .ready:
                print("Daemon listening on \(socketPathForHandler)")
            case .failed(let error):
                print("Listener failed: \(error)")
            default:
                break
            }
        }
        
        listener.start(queue: .global())
        
        // Keep running
        RunLoop.main.run()
    }
    
    func startDirectSocket() throws {
        // Use traditional BSD sockets for compatibility
        var addr = sockaddr_un()
        addr.sun_family = sa_family_t(AF_UNIX)
        
        let pathData = socketPath.data(using: .utf8)!
        pathData.withUnsafeBytes { bytes in
            memcpy(&addr.sun_path, bytes.baseAddress!, min(pathData.count, 107))
        }
        
        let sock = socket(AF_UNIX, SOCK_STREAM, 0)
        guard sock >= 0 else {
            throw DaemonError.signingFailed("Failed to create socket")
        }
        
        unlink(socketPath)
        
        let addrLen = socklen_t(MemoryLayout<sockaddr_un>.stride)
        let result = withUnsafePointer(to: &addr) { ptr in
            ptr.withMemoryRebound(to: sockaddr.self, capacity: 1) { addrPtr in
                bind(sock, addrPtr, addrLen)
            }
        }
        
        guard result == 0 else {
            close(sock)
            throw DaemonError.signingFailed("Failed to bind socket")
        }
        
        listen(sock, 10)
        print("Daemon listening on \(socketPath) (BSD socket)")
        
        // Accept connections in a loop
        while true {
            var clientAddr = sockaddr_un()
            var clientLen = socklen_t(MemoryLayout<sockaddr_un>.size)
            
            let clientSock = withUnsafeMutablePointer(to: &clientAddr) { ptr in
                ptr.withMemoryRebound(to: sockaddr.self, capacity: 1) { addrPtr in
                    accept(sock, addrPtr, &clientLen)
                }
            }
            
            guard clientSock >= 0 else { continue }
            
            // Handle in background
            DispatchQueue.global().async {
                self.handleDirectSocket(clientSock)
            }
        }
    }
    
    func handleDirectSocket(_ sock: Int32) {
        var buffer = [UInt8](repeating: 0, count: 4096)
        var data = Data()
        
        while true {
            let n = read(sock, &buffer, 4096)
            guard n > 0 else { break }
            data.append(buffer, count: n)
        }
        
        // Process request
        let response = processRequest(data: data)
        response.withUnsafeBytes { ptr in
            _ = write(sock, ptr.baseAddress!, ptr.count)
        }
        
        close(sock)
    }
    
    func handleConnection(_ connection: NWConnection) {
        connection.start(queue: .global())
        
        func receive() {
            connection.receive(minimumIncompleteLength: 1, maximumLength: 65536) { data, _, isComplete, error in
                if let data = data {
                    let response = self.processRequest(data: data)
                    connection.send(content: response, completion: .contentProcessed { _ in
                        if !isComplete {
                            receive()
                        }
                    })
                } else if isComplete || error != nil {
                    connection.cancel()
                }
            }
        }
        
        receive()
    }
    
    func processRequest(data: Data) -> Data {
        guard let request = try? JSONSerialization.jsonObject(with: data) as? [String: String],
              let cmd = request["cmd"] else {
            return makeErrorResponse("Invalid request")
        }
        
        switch cmd {
        case "jwt_sign":
            return handleJWTSign(request: request)
        case "ping":
            return makeSuccessResponse(["pong": true])
        case "stats":
            return makeSuccessResponse([
                "cached_keys": DaemonState.shared.keyCache.count
            ])
        case "clear_cache":
            DaemonState.shared.clearCache()
            return makeSuccessResponse(["cleared": true])
        default:
            return makeErrorResponse("Unknown command: \(cmd)")
        }
    }
    
    func handleJWTSign(request: [String: String]) -> Data {
        guard let issuerID = request["issuer_id"],
              let keyID = request["key_id"],
              let keyPath = request["key_path"] else {
            return makeErrorResponse("Missing required fields")
        }
        
        do {
            let privateKey = try DaemonState.shared.getKey(path: keyPath)
            let token = try generateJWT(issuerID: issuerID, keyID: keyID, privateKey: privateKey)
            
            return makeSuccessResponse([
                "token": token,
                "expires_in": Int(jwtTokenLifetime),
                "cached": DaemonState.shared.keyCache[keyPath] != nil
            ])
        } catch {
            return makeErrorResponse(error.localizedDescription)
        }
    }
    
    func makeSuccessResponse(_ data: [String: Any]) -> Data {
        var response = data
        response["success"] = true
        return (try? JSONSerialization.data(withJSONObject: response)) ?? Data()
    }
    
    func makeErrorResponse(_ message: String) -> Data {
        let response: [String: Any] = [
            "success": false,
            "error": message
        ]
        return (try? JSONSerialization.data(withJSONObject: response)) ?? Data()
    }
}

// MARK: - Command Interface

@main
struct DaemonCommand: ParsableCommand {
    static let configuration = CommandConfiguration(
        commandName: "asc-swift-daemon",
        abstract: "Long-running daemon for zero-latency Swift operations",
        version: "0.1.0"
    )
    
    @Option(name: .long, help: "Unix socket path")
    var socketPath: String = "/tmp/asc-swift-daemon.sock"
    
    @Flag(name: .long, help: "Stop running daemon")
    var stop: Bool = false
    
    @Flag(name: .long, help: "Check if daemon is running")
    var status: Bool = false
    
    mutating func run() throws {
        if status {
            checkStatus()
            return
        }
        
        if stop {
            try stopDaemon()
            return
        }
        
        // Start daemon
        let server = DaemonServer(socketPath: socketPath)
        print("Starting daemon on \(socketPath)")
        try server.start()
    }
    
    func checkStatus() {
        let fm = FileManager.default
        if fm.fileExists(atPath: socketPath) {
            // Try to connect and ping
            let request: [String: String] = ["cmd": "ping"]
            if let data = try? JSONSerialization.data(withJSONObject: request),
               let response = sendToDaemon(data: data),
               let json = try? JSONSerialization.jsonObject(with: response) as? [String: Any],
               json["pong"] as? Bool == true {
                print("Daemon is running on \(socketPath)")
                if let stats = json["cached_keys"] as? Int {
                    print("Cached keys: \(stats)")
                }
            } else {
                print("Socket exists but daemon not responding (stale socket?)")
            }
        } else {
            print("Daemon is not running")
        }
    }
    
    func stopDaemon() throws {
        let fm = FileManager.default
        if fm.fileExists(atPath: socketPath) {
            try fm.removeItem(atPath: socketPath)
            print("Daemon stopped")
        } else {
            print("Daemon was not running")
        }
    }
    
    func sendToDaemon(data: Data) -> Data? {
        // Simple socket client
        var addr = sockaddr_un()
        addr.sun_family = sa_family_t(AF_UNIX)
        
        let pathData = socketPath.data(using: .utf8)!
        pathData.withUnsafeBytes { bytes in
            memcpy(&addr.sun_path, bytes.baseAddress!, min(pathData.count, 107))
        }
        
        let sock = socket(AF_UNIX, SOCK_STREAM, 0)
        guard sock >= 0 else { return nil }
        
        let result = withUnsafePointer(to: &addr) { ptr in
            ptr.withMemoryRebound(to: sockaddr.self, capacity: 1) { addrPtr in
                connect(sock, addrPtr, socklen_t(MemoryLayout<sockaddr_un>.stride))
            }
        }
        
        guard result == 0 else {
            close(sock)
            return nil
        }
        
        data.withUnsafeBytes { ptr in
            _ = write(sock, ptr.baseAddress!, ptr.count)
        }
        shutdown(sock, SHUT_WR)
        
        var buffer = [UInt8](repeating: 0, count: 4096)
        var response = Data()
        
        while true {
            let n = read(sock, &buffer, 4096)
            guard n > 0 else { break }
            response.append(buffer, count: n)
        }
        
        close(sock)
        return response
    }
}
