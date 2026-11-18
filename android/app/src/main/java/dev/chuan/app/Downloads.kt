package dev.chuan.app

import android.content.Context
import android.media.MediaScannerConnection
import android.os.Environment
import android.os.Handler
import android.os.Looper
import android.util.Base64
import android.webkit.JavascriptInterface
import android.widget.Toast
import java.io.File
import java.io.FileOutputStream
import java.io.OutputStream

/**
 * Writes received files on behalf of the wasm client.
 *
 * The browser build of the app hands a Blob to a download link; a WebView has
 * no download manager for `blob:` URLs, so `bridge.js` streams the blob here in
 * base64 chunks instead. Files land in the app's external downloads directory,
 * which needs no permission and is visible to file managers.
 */
class Downloads(private val context: Context) {

    private var stream: OutputStream? = null
    private var target: File? = null
    private var received = 0L

    @JavascriptInterface
    fun beginFile(name: String): Boolean {
        abortFile()
        val dir = context.getExternalFilesDir(Environment.DIRECTORY_DOWNLOADS)
            ?: File(context.filesDir, "downloads")
        if (!dir.exists() && !dir.mkdirs()) return false
        val file = unique(dir, sanitize(name))
        return try {
            stream = FileOutputStream(file)
            target = file
            received = 0L
            true
        } catch (_: Exception) {
            stream = null
            target = null
            false
        }
    }

    @JavascriptInterface
    fun appendChunk(base64: String): Boolean {
        val out = stream ?: return false
        return try {
            val chunk = Base64.decode(base64, Base64.DEFAULT)
            out.write(chunk)
            received += chunk.size
            true
        } catch (_: Exception) {
            false
        }
    }

    @JavascriptInterface
    fun finishFile(): String {
        val out = stream ?: return ""
        val file = target
        return try {
            out.flush()
            out.close()
            stream = null
            target = null
            if (file != null) {
                MediaScannerConnection.scanFile(
                    context, arrayOf(file.absolutePath), null, null
                )
                toast("已保存 ${file.name}")
                file.absolutePath
            } else {
                ""
            }
        } catch (_: Exception) {
            ""
        }
    }

    @JavascriptInterface
    fun abortFile() {
        try {
            stream?.close()
        } catch (_: Exception) {
        }
        stream = null
        target?.delete()
        target = null
    }

    @JavascriptInterface
    fun downloadedBytes(): Long = received

    private fun sanitize(name: String): String {
        val base = name.substringAfterLast('/').substringAfterLast('\\').trim()
        val cleaned = base.map { if (it.isISOControl() || it == ':' || it == '*') '_' else it }
            .joinToString("")
        return if (cleaned.isEmpty() || cleaned == "." || cleaned == "..") "未命名文件" else cleaned
    }

    private fun unique(dir: File, name: String): File {
        var candidate = File(dir, name)
        if (!candidate.exists()) return candidate
        val dot = name.lastIndexOf('.')
        val stem = if (dot > 0) name.substring(0, dot) else name
        val ext = if (dot > 0) name.substring(dot) else ""
        var i = 1
        while (candidate.exists()) {
            candidate = File(dir, "$stem ($i)$ext")
            i++
        }
        return candidate
    }

    private fun toast(message: String) {
        Handler(Looper.getMainLooper()).post {
            Toast.makeText(context, message, Toast.LENGTH_SHORT).show()
        }
    }
}
