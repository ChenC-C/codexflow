package com.example.codexflow_flutter

import android.app.Activity
import android.content.ContentValues
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.os.Environment
import android.os.Handler
import android.os.Looper
import android.provider.MediaStore
import android.provider.OpenableColumns
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel
import java.io.File
import java.io.FileOutputStream
import java.io.IOException
import java.io.InputStream
import java.net.HttpURLConnection
import java.net.URL

class MainActivity : FlutterActivity() {
    private var pendingFilePickResult: MethodChannel.Result? = null

    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)
        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, "codexflow/open_url")
            .setMethodCallHandler { call, result ->
                if (call.method != "openUrl") {
                    result.notImplemented()
                    return@setMethodCallHandler
                }

                val url = call.argument<String>("url")?.trim().orEmpty()
                if (url.isEmpty()) {
                    result.success(false)
                    return@setMethodCallHandler
                }

                try {
                    val intent = Intent(Intent.ACTION_VIEW, Uri.parse(url))
                    intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
                    startActivity(intent)
                    result.success(true)
                } catch (_: Exception) {
                    result.success(false)
                }
            }
        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, "codexflow/download_file")
            .setMethodCallHandler { call, result ->
                if (call.method != "downloadFile") {
                    result.notImplemented()
                    return@setMethodCallHandler
                }

                downloadFile(
                    call.argument<String>("url"),
                    call.argument<String>("fileName"),
                    call.argument<String>("mimeType"),
                    result
                )
            }
        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, "codexflow/file_picker")
            .setMethodCallHandler { call, result ->
                if (call.method != "pickFile") {
                    result.notImplemented()
                    return@setMethodCallHandler
                }
                if (pendingFilePickResult != null) {
                    result.error("busy", "A file picker is already open.", null)
                    return@setMethodCallHandler
                }

                pendingFilePickResult = result
                try {
                    val intent = Intent(Intent.ACTION_OPEN_DOCUMENT).apply {
                        addCategory(Intent.CATEGORY_OPENABLE)
                        type = "*/*"
                    }
                    startActivityForResult(intent, REQUEST_PICK_FILE)
                } catch (error: Exception) {
                    pendingFilePickResult = null
                    result.error("unavailable", error.message, null)
                }
            }
    }

    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        super.onActivityResult(requestCode, resultCode, data)
        if (requestCode != REQUEST_PICK_FILE) {
            return
        }

        val result = pendingFilePickResult ?: return
        pendingFilePickResult = null
        if (resultCode != Activity.RESULT_OK || data?.data == null) {
            result.success(null)
            return
        }

        val uri = data.data!!
        try {
            val bytes = contentResolver.openInputStream(uri)?.use { it.readBytes() }
            if (bytes == null) {
                result.error("read_failed", "Unable to read selected file.", null)
                return
            }
            result.success(
                mapOf(
                    "name" to displayNameFor(uri),
                    "bytes" to bytes,
                )
            )
        } catch (error: Exception) {
            result.error("read_failed", error.message, null)
        }
    }

    private fun downloadFile(
        rawUrl: String?,
        rawFileName: String?,
        rawMimeType: String?,
        result: MethodChannel.Result
    ) {
        val url = rawUrl?.trim().orEmpty()
        if (url.isEmpty()) {
            result.success(downloadFailure("missing download URL"))
            return
        }

        Thread {
            val response = try {
                downloadToPublicDownloads(url, rawFileName, rawMimeType)
            } catch (error: Exception) {
                downloadFailure(error.message ?: "download failed")
            }
            Handler(Looper.getMainLooper()).post {
                result.success(response)
            }
        }.start()
    }

    private fun downloadToPublicDownloads(
        urlString: String,
        rawFileName: String?,
        rawMimeType: String?
    ): Map<String, Any> {
        val uri = Uri.parse(urlString)
        val scheme = uri.scheme?.lowercase().orEmpty()
        if (scheme != "http" && scheme != "https") {
            throw IOException("unsupported download URL")
        }

        val connection = (URL(urlString).openConnection() as HttpURLConnection).apply {
            connectTimeout = 15000
            readTimeout = 45000
            instanceFollowRedirects = true
            requestMethod = "GET"
            setRequestProperty("Accept", "*/*")
            setRequestProperty("User-Agent", "CodexFlow Android")
        }

        try {
            val statusCode = connection.responseCode
            if (statusCode !in 200..299) {
                throw IOException("HTTP $statusCode")
            }

            val fileName = safeFileName(rawFileName)
            val mimeType = rawMimeType?.trim()?.takeIf { it.isNotEmpty() }
                ?: connection.contentType?.substringBefore(';')?.trim()?.takeIf { it.isNotEmpty() }
                ?: "application/octet-stream"
            val saved = connection.inputStream.use { input ->
                saveToPublicDownloads(fileName, mimeType, input)
            }

            return mapOf(
                "ok" to true,
                "fileName" to fileName,
                "path" to saved.path,
                "uri" to saved.uri.toString()
            )
        } finally {
            connection.disconnect()
        }
    }

    private fun saveToPublicDownloads(
        fileName: String,
        mimeType: String,
        input: InputStream
    ): SavedDownload {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            return saveToMediaStoreDownloads(fileName, mimeType, input)
        }

        @Suppress("DEPRECATION")
        val dir = Environment.getExternalStoragePublicDirectory(Environment.DIRECTORY_DOWNLOADS)
        if (!dir.exists() && !dir.mkdirs()) {
            throw IOException("cannot create Downloads directory")
        }
        val file = uniqueDownloadFile(dir, fileName)
        FileOutputStream(file).use { output ->
            input.copyTo(output)
        }
        return SavedDownload(uri = Uri.fromFile(file), path = file.absolutePath)
    }

    private fun saveToMediaStoreDownloads(
        fileName: String,
        mimeType: String,
        input: InputStream
    ): SavedDownload {
        val bytes = input.readBytes()
        var lastError: Exception? = null
        for (index in 0..20) {
            val candidateName = numberedFileName(fileName, index)
            val values = ContentValues().apply {
                put(MediaStore.Downloads.DISPLAY_NAME, candidateName)
                put(MediaStore.Downloads.MIME_TYPE, mimeType)
                put(MediaStore.Downloads.RELATIVE_PATH, Environment.DIRECTORY_DOWNLOADS)
                put(MediaStore.Downloads.IS_PENDING, 1)
            }
            val uri = try {
                contentResolver.insert(MediaStore.Downloads.EXTERNAL_CONTENT_URI, values)
            } catch (error: Exception) {
                lastError = error
                if (canRetryWithAnotherName(error)) {
                    continue
                }
                throw error
            } ?: throw IOException("cannot create download file")

            var completed = false
            try {
                contentResolver.openOutputStream(uri)?.use { output ->
                    output.write(bytes)
                } ?: throw IOException("cannot open download file")

                values.clear()
                values.put(MediaStore.Downloads.IS_PENDING, 0)
                contentResolver.update(uri, values, null, null)
                completed = true
                return SavedDownload(
                    uri = uri,
                    path = "${Environment.DIRECTORY_DOWNLOADS}/$candidateName"
                )
            } catch (error: Exception) {
                lastError = error
                if (!canRetryWithAnotherName(error)) {
                    throw error
                }
            } finally {
                if (!completed) {
                    contentResolver.delete(uri, null, null)
                }
            }
        }

        throw IOException("cannot create unique download file", lastError)
    }

    private fun displayNameFor(uri: Uri): String {
        contentResolver.query(uri, null, null, null, null)?.use { cursor ->
            val index = cursor.getColumnIndex(OpenableColumns.DISPLAY_NAME)
            if (index >= 0 && cursor.moveToFirst()) {
                val name = cursor.getString(index)?.trim().orEmpty()
                if (name.isNotEmpty()) {
                    return name
                }
            }
        }
        return uri.lastPathSegment?.substringAfterLast('/')?.trim()?.takeIf { it.isNotEmpty() }
            ?: "attachment"
    }

    private fun safeFileName(raw: String?): String {
        val cleaned = raw?.trim().orEmpty().replace(Regex("[\\\\/:*?\"<>|\\p{Cntrl}]+"), "_")
        return cleaned.ifEmpty { "codexflow-download" }
    }

    private fun uniqueDownloadFile(dir: File, fileName: String): File {
        var candidate = File(dir, numberedFileName(fileName, 0))
        if (!candidate.exists()) {
            return candidate
        }

        var index = 1
        while (candidate.exists()) {
            candidate = File(dir, numberedFileName(fileName, index))
            index += 1
        }
        return candidate
    }

    private fun numberedFileName(fileName: String, index: Int): String {
        if (index <= 0) {
            return fileName
        }
        val dotIndex = fileName.lastIndexOf('.')
        val baseName = if (dotIndex > 0) fileName.substring(0, dotIndex) else fileName
        val extension = if (dotIndex > 0) fileName.substring(dotIndex) else ""
        return "$baseName ($index)$extension"
    }

    private fun canRetryWithAnotherName(error: Exception): Boolean {
        val message = error.message.orEmpty()
        return message.contains("UNIQUE constraint", ignoreCase = true) ||
            message.contains("files._data", ignoreCase = true) ||
            message.contains("File exists", ignoreCase = true)
    }

    private fun downloadFailure(message: String): Map<String, Any> {
        return mapOf("ok" to false, "error" to message)
    }

    private data class SavedDownload(val uri: Uri, val path: String)

    companion object {
        private const val REQUEST_PICK_FILE = 4102
    }
}
