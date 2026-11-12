package xyz.vandijke.sodapop

import android.app.NotificationChannel
import android.app.NotificationManager
import android.os.Build
import android.os.Bundle
import android.webkit.CookieManager
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel

class MainActivity : FlutterActivity() {
    private val channelName = "xyz.vandijke.sodapop/cookies"

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        createNotificationChannel()
    }

    // FCM payloads target this channel by id. Creating it up front keeps the
    // look stable instead of letting Android fall back to "Miscellaneous".
    private fun createNotificationChannel() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return
        val channel = NotificationChannel(
            "sodapop_default",
            "Activity",
            NotificationManager.IMPORTANCE_DEFAULT,
        ).apply {
            description = "Toasts, comments, friend requests and new check-ins"
        }
        getSystemService(NotificationManager::class.java)
            .createNotificationChannel(channel)
    }

    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)

        // sd_session is HttpOnly, so the OIDC WebView cannot expose it to JS.
        // Dart asks the native cookie store for the signed-in session instead.
        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, channelName)
            .setMethodCallHandler { call, result ->
                when (call.method) {
                    "getCookies" -> {
                        val url = call.argument<String>("url")
                        if (url.isNullOrEmpty()) {
                            result.error("invalid_url", "A url argument is required", null)
                        } else {
                            result.success(CookieManager.getInstance().getCookie(url))
                        }
                    }
                    "clearCookies" -> {
                        CookieManager.getInstance().removeAllCookies(null)
                        CookieManager.getInstance().flush()
                        result.success(null)
                    }
                    else -> result.notImplemented()
                }
            }
    }
}
