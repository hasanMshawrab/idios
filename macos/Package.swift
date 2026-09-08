// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "idios",
    platforms: [.macOS(.v15)],
    products: [
        .library(name: "IdiosAPI", targets: ["IdiosAPI"]),
        .library(name: "IdiosModel", targets: ["IdiosModel"]),
    ],
    dependencies: [
        .package(url: "https://github.com/apple/swift-openapi-generator", from: "1.10.0"),
        .package(url: "https://github.com/apple/swift-openapi-runtime", from: "1.8.0"),
        .package(url: "https://github.com/apple/swift-openapi-urlsession", from: "1.1.0"),
    ],
    targets: [
        .target(
            name: "IdiosAPI",
            dependencies: [
                .product(name: "OpenAPIRuntime", package: "swift-openapi-runtime"),
                .product(name: "OpenAPIURLSession", package: "swift-openapi-urlsession"),
            ],
            plugins: [
                .plugin(name: "OpenAPIGenerator", package: "swift-openapi-generator"),
            ]
        ),
        .target(name: "IdiosModel", dependencies: ["IdiosAPI"]),
        .testTarget(name: "IdiosModelTests", dependencies: ["IdiosModel"]),
    ]
)
