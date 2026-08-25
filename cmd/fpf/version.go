package main

// version is set at build time via ldflags: -X main.version=<version>
// If empty, resolvePackageVersion will fallback to reading package.json.
var version = ""
