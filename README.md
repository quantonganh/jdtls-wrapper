# jdtls-wrapper

A Java language server wrapper for [Helix editor](https://helix-editor.com/).

## Why?

By default, Helix cannot decompile Java class file. If you simply install `jdtls` and configure it normally:

```toml
[[language]]
name = "java"
language-servers = [ "jdtls" ]
indent = { tab-width = 4, unit = "    " }

[language-server.jdtls]
command = "jdtls"
```

Pressing `gd` (Go to definition) will cause Helix to return "No definition found".

It took me a few hours to find out that the client must enable `classFileContentsSupport` during initialization:

```toml
[language-server.jdtls.config]
extendedClientCapabilities.classFileContentsSupport = true
```

Once enabled, The server responses with something like this:

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "result": [
    {
      "uri": "jdt://contents/java.base/java.util/ArrayList.class?=karls-languages/%5C/opt%5C/homebrew%5C/Cellar%5C/openjdk%5C@21%5C/21.0.7%5C/libexec%5C/openjdk.jdk%5C/Contents%5C/Home%5C/lib%5C/jrt-fs.jar%60java.base=/javadoc_location=/https:%5C/%5C/docs.oracle.com%5C/en%5C/java%5C/javase%5C/21%5C/docs%5C/api%5C/=/%3Cjava.util(ArrayList.class",
      "range": {
        "start": {
          "line": 167,
          "character": 11
        },
        "end": {
          "line": 167,
          "character": 20
        }
      }
    }
  ]
}
```

The problem is that the response uses `jdt://` URL scheme, which currently Helix does not support:

```
2025-08-08T14:52:01.392 helix_term::commands::lsp [WARN] discarding invalid or unsupported URI: unsupported scheme 'jdt' in URL jdt://contents/java.base/java.util/ArrayList.class...    
```

This wrapper exists to solve that issue. It sits between Helix and the `jdtls` server, translates `jdt://` to `file://` URIs and displays the decompiled class file.

## Installation

### Via homebrew

```
brew install quantonganh/tap/jdtls-wrapper
```

### Via go

```
go install github.com/quantonganh/jdtls-wrapper@latest
```

## Usage

Simply change the language server command to `jdtls-wrapper`:

```toml
[[language]]
name = "java"
language-servers = [ "jdtls" ]
indent = { tab-width = 4, unit = "    " }

[language-server.jdtls]
command = "jdtls-wrapper"
args = ["--jvm-arg=-javaagent:/Users/quantong/.lombok/lombok.jar"]

[language-server.jdtls.config]
java.inlayHints.parameterNames.enabled = "all"
extendedClientCapabilities.classFileContentsSupport = true
```
