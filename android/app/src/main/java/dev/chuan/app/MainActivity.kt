package dev.chuan.app

import android.annotation.SuppressLint
import android.app.Activity
import android.content.Intent
import android.graphics.Color
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.view.View
import android.view.ViewGroup
import android.webkit.ValueCallback
import android.webkit.WebChromeClient
import android.webkit.WebResourceRequest
import android.webkit.WebSettings
import android.webkit.WebView
import android.webkit.WebViewClient

/**
 * The Android shell: a WebView showing the same wasm client the browser build
 * ships, served over loopback so `fetch()` of `app.wasm` works.
 */
class MainActivity : Activity() {

    private lateinit var web: WebView
    private lateinit var server: AssetServer
    private var filePathCallback: ValueCallback<Array<Uri>>? = null

    @SuppressLint("SetJavaScriptEnabled")
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        server = AssetServer(assets)
        val port = server.start()

        web = WebView(this)
        web.setBackgroundColor(Color.parseColor("#09090b"))
        web.setLayerType(View.LAYER_TYPE_HARDWARE, null)

        web.settings.apply {
            javaScriptEnabled = true
            domStorageEnabled = true
            databaseEnabled = true
            mediaPlaybackRequiresUserGesture = false
            useWideViewPort = true
            loadWithOverviewMode = true
            allowFileAccess = false
            allowContentAccess = false
            mixedContentMode = WebSettings.MIXED_CONTENT_NEVER_ALLOW
            cacheMode = WebSettings.LOAD_NO_CACHE
            userAgentString = "$userAgentString ChuanMygo/0.1.0"
        }

        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.KITKAT) {
            WebView.setWebContentsDebuggingEnabled(BuildConfig.DEBUG)
        }

        web.addJavascriptInterface(Downloads(applicationContext), "chuanAndroid")

        web.webChromeClient = object : WebChromeClient() {
            override fun onShowFileChooser(
                view: WebView,
                callback: ValueCallback<Array<Uri>>,
                params: FileChooserParams
            ): Boolean {
                filePathCallback?.onReceiveValue(null)
                filePathCallback = callback
                return try {
                    startActivityForResult(params.createIntent(), REQ_PICK_FILES)
                    true
                } catch (_: Exception) {
                    filePathCallback = null
                    false
                }
            }
        }

        web.webViewClient = object : WebViewClient() {
            override fun shouldOverrideUrlLoading(
                view: WebView,
                request: WebResourceRequest
            ): Boolean {
                val uri = request.url
                val scheme = uri.scheme ?: return false
                if (scheme == "http" || scheme == "https") {
                    if (uri.host == "127.0.0.1") return false
                    // Anything not ours opens in the real browser.
                    startActivity(Intent(Intent.ACTION_VIEW, uri))
                    return true
                }
                return false
            }
        }

        setContentView(
            web,
            ViewGroup.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT,
                ViewGroup.LayoutParams.MATCH_PARENT
            )
        )

        web.loadUrl("http://127.0.0.1:$port/index.html")
    }

    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        if (requestCode == REQ_PICK_FILES) {
            val callback = filePathCallback ?: return
            filePathCallback = null
            val uris = if (resultCode == RESULT_OK && data != null) {
                WebChromeClient.FileChooserParams.parseResult(resultCode, data)
            } else {
                null
            }
            callback.onReceiveValue(uris)
            return
        }
        super.onActivityResult(requestCode, resultCode, data)
    }

    override fun onBackPressed() {
        if (web.canGoBack()) {
            web.goBack()
            return
        }
        super.onBackPressed()
    }

    override fun onDestroy() {
        try {
            web.destroy()
        } catch (_: Exception) {
        }
        server.stop()
        super.onDestroy()
    }

    private companion object {
        const val REQ_PICK_FILES = 1001
    }
}
