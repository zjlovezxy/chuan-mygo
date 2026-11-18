package dev.chuan.app

import android.content.res.AssetManager
import java.io.BufferedOutputStream
import java.io.IOException
import java.io.InputStream
import java.net.InetAddress
import java.net.ServerSocket
import java.net.Socket
import kotlin.concurrent.thread

/**
 * Serves the packaged wasm client over loopback HTTP.
 *
 * A WebView cannot `fetch()` from `file://`, and `WebAssembly` needs a real
 * response with the `application/wasm` type, so the assets are exposed on
 * 127.0.0.1 instead of loaded from `file:///android_asset`.
 */
class AssetServer(private val assets: AssetManager) {

    private var server: ServerSocket? = null

    var port: Int = 0
        private set

    fun start(): Int {
        val socket = ServerSocket(0, 16, InetAddress.getByName("127.0.0.1"))
        server = socket
        port = socket.localPort
        thread(name = "chuan-assets", isDaemon = true) {
            while (!socket.isClosed) {
                val client = try {
                    socket.accept()
                } catch (_: IOException) {
                    break
                }
                thread(isDaemon = true, name = "chuan-assets-conn") { serve(client) }
            }
        }
        return port
    }

    fun stop() {
        try {
            server?.close()
        } catch (_: IOException) {
        }
        server = null
    }

    private fun serve(client: Socket) {
        client.use { socket ->
            try {
                val input = socket.getInputStream()
                val requestLine = readLine(input) ?: return
                val parts = requestLine.split(' ')
                if (parts.size < 2) return
                var path = parts[1].substringBefore('?').substringBefore('#')
                if (path == "/" || path.isEmpty()) path = "/index.html"
                if (path.endsWith("/")) path += "index.html"

                val assetPath = "web" + path
                val stream = open(assetPath)
                val out = BufferedOutputStream(socket.getOutputStream())
                if (stream == null) {
                    val body = "Not found: $path".toByteArray()
                    out.write(headers(404, "text/plain; charset=utf-8", body.size).toByteArray())
                    out.write(body)
                } else {
                    val body = stream.use { it.readBytes() }
                    out.write(headers(200, mimeOf(assetPath), body.size).toByteArray())
                    out.write(body)
                }
                out.flush()
            } catch (_: IOException) {
                // The page closed the connection; nothing to report.
            }
        }
    }

    private fun open(assetPath: String): InputStream? = try {
        assets.open(assetPath)
    } catch (_: IOException) {
        null
    }

    private fun headers(status: Int, type: String, length: Int): String = buildString {
        append("HTTP/1.1 ").append(status).append(' ')
        append(if (status == 200) "OK" else "Not Found").append("\r\n")
        append("Content-Type: ").append(type).append("\r\n")
        append("Content-Length: ").append(length).append("\r\n")
        append("Cache-Control: no-store\r\n")
        append("Connection: close\r\n\r\n")
    }

    private fun readLine(input: InputStream): String? {
        val sb = StringBuilder()
        while (true) {
            val b = input.read()
            if (b == -1) return if (sb.isEmpty()) null else sb.toString()
            if (b == '\n'.code) break
            if (b != '\r'.code) sb.append(b.toChar())
            if (sb.length > 8192) return null
        }
        return sb.toString()
    }

    private fun mimeOf(path: String): String = when (path.substringAfterLast('.', "").lowercase()) {
        "html", "htm" -> "text/html; charset=utf-8"
        "js", "mjs" -> "text/javascript; charset=utf-8"
        "css" -> "text/css; charset=utf-8"
        "json" -> "application/json; charset=utf-8"
        "wasm" -> "application/wasm"
        "png" -> "image/png"
        "svg" -> "image/svg+xml"
        "ico" -> "image/x-icon"
        "woff2" -> "font/woff2"
        "txt", "md" -> "text/plain; charset=utf-8"
        else -> "application/octet-stream"
    }
}
