import Foundation
import CoreGraphics
import ImageIO
import UniformTypeIdentifiers
import Darwin

final class MobilePhotoWork {
    private let lock = NSLock()
    private var cancelled = false

    func cancel() {
        lock.lock()
        cancelled = true
        lock.unlock()
    }

    func check() throws {
        lock.lock()
        let stopped = cancelled
        lock.unlock()
        if stopped { throw CocoaError(.userCancelled) }
    }
}

enum MobilePhotoAccess {
    static let maximumOriginalBytes = 20 * 1024 * 1024

    static func readOriginal(_ url: URL, work: MobilePhotoWork) throws -> Data {
        precondition(!Thread.isMainThread)
        try work.check()
        guard url.isFileURL else { throw MobileFileAccess.Failure.invalid }
        let metadata = try url.resourceValues(forKeys: [.isRegularFileKey, .isSymbolicLinkKey])
        guard metadata.isRegularFile == true, metadata.isSymbolicLink != true else { throw MobileFileAccess.Failure.unsupported }
        let descriptor = url.withUnsafeFileSystemRepresentation { path in
            path.map { Darwin.open($0, O_RDONLY | O_NOFOLLOW | O_NONBLOCK) } ?? -1
        }
        guard descriptor >= 0 else { throw NSError(domain: NSPOSIXErrorDomain, code: Int(errno)) }
        let handle = FileHandle(fileDescriptor: descriptor, closeOnDealloc: true)
        defer { try? handle.close() }
        var attributes = stat()
        guard fstat(descriptor, &attributes) == 0 else { throw MobileFileAccess.Failure.invalid }
        guard attributes.st_mode & S_IFMT == S_IFREG else { throw MobileFileAccess.Failure.unsupported }
        guard attributes.st_size > 0, attributes.st_size <= maximumOriginalBytes else { throw MobileFileAccess.Failure.tooLarge }
        let size = Int(attributes.st_size)
        var bytes = Data()
        while bytes.count < size {
            try work.check()
            guard let chunk = try handle.read(upToCount: min(64 * 1024, size - bytes.count)), !chunk.isEmpty else {
                throw MobileFileAccess.Failure.invalid
            }
            bytes.append(chunk)
        }
        guard fstat(descriptor, &attributes) == 0, attributes.st_size == size else { throw MobileFileAccess.Failure.invalid }
        try work.check()
        return bytes
    }

    static func jpeg(_ bytes: Data, photo: MobilePhoto, work: MobilePhotoWork) throws -> MobileFilePayload {
        precondition(!Thread.isMainThread)
        try work.check()
        guard photo.isValid, !bytes.isEmpty, bytes.count <= maximumOriginalBytes,
              let source = CGImageSourceCreateWithData(bytes as CFData, [kCGImageSourceShouldCache: false] as CFDictionary),
              CGImageSourceGetCount(source) > 0, CGImageSourceGetStatusAtIndex(source, 0) == .statusComplete,
              let thumbnail = CGImageSourceCreateThumbnailAtIndex(source, 0, [
                kCGImageSourceCreateThumbnailFromImageAlways: true,
                kCGImageSourceCreateThumbnailWithTransform: true,
                kCGImageSourceThumbnailMaxPixelSize: photo.maxDimension,
                kCGImageSourceShouldCacheImmediately: true
              ] as CFDictionary) else { throw MobileFileAccess.Failure.invalid }
        return try jpeg(thumbnail, orientation: .up, photo: photo, work: work)
    }

    static func jpeg(_ image: CGImage, orientation: CGImagePropertyOrientation, photo: MobilePhoto,
                     work: MobilePhotoWork) throws -> MobileFilePayload {
        precondition(!Thread.isMainThread)
        guard photo.isValid else { throw MobileFileAccess.Failure.invalid }
        try work.check()
        var raster = try normalized(image, orientation: orientation, maxDimension: photo.maxDimension)
        while true {
            for quality in [0.9, 0.65, 0.4, 0.15] {
                try work.check()
                let bytes = try autoreleasepool { () throws -> Data in
                    let buffer = NSMutableData()
                    guard let destination = CGImageDestinationCreateWithData(buffer, UTType.jpeg.identifier as CFString, 1, nil) else {
                        throw MobileFileAccess.Failure.invalid
                    }
                    CGImageDestinationAddImage(destination, raster, [kCGImageDestinationLossyCompressionQuality: quality] as CFDictionary)
                    guard CGImageDestinationFinalize(destination) else { throw MobileFileAccess.Failure.invalid }
                    return buffer as Data
                }
                if bytes.count <= photo.maxBytes {
                    try work.check()
                    return MobileFilePayload(name: photo.source == .camera ? "capture.jpg" : "photo.jpg", mediaType: "image/jpeg", data: bytes.base64EncodedString())
                }
            }
            let dimension = max(raster.width, raster.height)
            guard dimension > 1 else { throw MobileFileAccess.Failure.tooLarge }
            raster = try normalized(raster, orientation: .up, maxDimension: max(1, dimension / 2))
        }
    }

    static func normalized(_ image: CGImage, orientation: CGImagePropertyOrientation, maxDimension: Int) throws -> CGImage {
        precondition(!Thread.isMainThread)
        guard (1...2048).contains(maxDimension) else { throw MobileFileAccess.Failure.invalid }
        let width = CGFloat(image.width)
        let height = CGFloat(image.height)
        let rotated = [.left, .leftMirrored, .right, .rightMirrored].contains(orientation)
        let outputWidth = rotated ? height : width
        let outputHeight = rotated ? width : height
        let scale = min(1, CGFloat(maxDimension) / max(outputWidth, outputHeight))
        let targetWidth = max(1, Int((outputWidth * scale).rounded(.down)))
        let targetHeight = max(1, Int((outputHeight * scale).rounded(.down)))
        guard let colorSpace = CGColorSpace(name: CGColorSpace.sRGB),
              let context = CGContext(data: nil, width: targetWidth, height: targetHeight, bitsPerComponent: 8, bytesPerRow: 0,
                                      space: colorSpace, bitmapInfo: CGImageAlphaInfo.noneSkipLast.rawValue) else {
            throw MobileFileAccess.Failure.invalid
        }
        context.setFillColor(CGColor(gray: 1, alpha: 1))
        context.fill(CGRect(x: 0, y: 0, width: targetWidth, height: targetHeight))
        context.scaleBy(x: CGFloat(targetWidth) / outputWidth, y: CGFloat(targetHeight) / outputHeight)
        let transform: CGAffineTransform
        switch orientation {
        case .up: transform = .identity
        case .upMirrored: transform = CGAffineTransform(a: -1, b: 0, c: 0, d: 1, tx: width, ty: 0)
        case .down: transform = CGAffineTransform(a: -1, b: 0, c: 0, d: -1, tx: width, ty: height)
        case .downMirrored: transform = CGAffineTransform(a: 1, b: 0, c: 0, d: -1, tx: 0, ty: height)
        case .leftMirrored: transform = CGAffineTransform(a: 0, b: -1, c: -1, d: 0, tx: height, ty: width)
        case .right: transform = CGAffineTransform(a: 0, b: -1, c: 1, d: 0, tx: 0, ty: width)
        case .rightMirrored: transform = CGAffineTransform(a: 0, b: 1, c: 1, d: 0, tx: 0, ty: 0)
        case .left: transform = CGAffineTransform(a: 0, b: 1, c: -1, d: 0, tx: height, ty: 0)
        @unknown default: throw MobileFileAccess.Failure.invalid
        }
        context.concatenate(transform)
        context.interpolationQuality = .high
        context.draw(image, in: CGRect(x: 0, y: 0, width: width, height: height))
        guard let result = context.makeImage() else { throw MobileFileAccess.Failure.invalid }
        return result
    }

    static func status(for error: Error) -> MobileFileStatus {
        var underlying = error as NSError
        for _ in 0..<8 {
            if underlying.domain == NSCocoaErrorDomain, underlying.code == CocoaError.userCancelled.rawValue { return .cancelled }
            if underlying.domain == NSURLErrorDomain, underlying.code == NSURLErrorCancelled { return .cancelled }
            guard let next = underlying.userInfo[NSUnderlyingErrorKey] as? NSError else { break }
            underlying = next
        }
        return MobileFileAccess.status(for: error)
    }
}
