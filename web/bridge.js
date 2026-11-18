// bridge.js is the browser side of the wasm client: it hands File objects to
// Go (which cannot hold them) and saves received blobs, either through the
// browser download or through the Android shell's file writer.
(function () {
  "use strict";

  var files = new Map();
  var seq = 0;

  function pickFiles() {
    return new Promise(function (resolve) {
      var input = document.createElement("input");
      input.type = "file";
      input.multiple = true;
      input.style.display = "none";
      document.body.appendChild(input);

      var settled = false;
      function done(list) {
        if (settled) return;
        settled = true;
        input.remove();
        resolve(list);
      }

      input.addEventListener("change", function () {
        var out = [];
        var chosen = Array.prototype.slice.call(input.files || []);
        for (var i = 0; i < chosen.length; i++) {
          var file = chosen[i];
          var id = "f" + ++seq;
          files.set(id, file);
          out.push({ id: id, name: file.name, size: file.size, type: file.type || "" });
        }
        done(out);
      });

      // A cancelled dialog fires no change event on most browsers; when focus
      // comes back without files, resolve with an empty list.
      window.addEventListener(
        "focus",
        function () {
          setTimeout(function () {
            if (!settled && (!input.files || input.files.length === 0)) done([]);
          }, 600);
        },
        { once: true }
      );

      input.click();
    });
  }

  function read(id, offset, length) {
    return new Promise(function (resolve, reject) {
      var file = files.get(id);
      if (!file) {
        reject(new Error("文件已失效，请重新选择"));
        return;
      }
      var slice = file.slice(offset, offset + length);
      if (slice.arrayBuffer) {
        slice
          .arrayBuffer()
          .then(function (buf) {
            resolve(new Uint8Array(buf));
          })
          .catch(reject);
        return;
      }
      var reader = new FileReader();
      reader.onload = function () {
        resolve(new Uint8Array(reader.result));
      };
      reader.onerror = function () {
        reject(reader.error || new Error("读取文件失败"));
      };
      reader.readAsArrayBuffer(slice);
    });
  }

  function release(id) {
    files.delete(id);
  }

  // base64 encodes bytes in slices small enough for String.fromCharCode.apply.
  function toBase64(bytes) {
    var CHUNK = 0x8000;
    var parts = [];
    for (var i = 0; i < bytes.length; i += CHUNK) {
      parts.push(String.fromCharCode.apply(null, bytes.subarray(i, i + CHUNK)));
    }
    return btoa(parts.join(""));
  }

  // saveBlob writes a received blob through the Android shell, which streams it
  // into the app's downloads directory. Desktop browsers keep the plain
  // <a download> path in Go, so this returns false there.
  function saveBlob(url, name) {
    var android = window.chuanAndroid;
    if (!android || typeof android.beginFile !== "function") return false;

    (async function () {
      var response = await fetch(url);
      if (!response.ok || !response.body) throw new Error("无法读取临时文件");
      if (!android.beginFile(name)) throw new Error("无法创建目标文件");
      var reader = response.body.getReader();
      var pending = new Uint8Array(0);
      var FLUSH = 256 * 1024;
      for (;;) {
        var step = await reader.read();
        if (step.done) break;
        var merged = new Uint8Array(pending.length + step.value.length);
        merged.set(pending, 0);
        merged.set(step.value, pending.length);
        pending = merged;
        if (pending.length >= FLUSH) {
          if (!android.appendChunk(toBase64(pending))) throw new Error("写入失败");
          pending = new Uint8Array(0);
        }
      }
      if (pending.length > 0 && !android.appendChunk(toBase64(pending))) {
        throw new Error("写入失败");
      }
      android.finishFile();
    })().catch(function (err) {
      if (android.abortFile) android.abortFile();
      if (window.chuanAlert) window.chuanAlert(String(err));
    });

    return true;
  }

  if (!window.chuanAlert) {
    window.chuanAlert = function (message) {
      window.alert(message);
    };
  }

  window.chuanBridge = {
    pickFiles: pickFiles,
    read: read,
    release: release,
    saveBlob: saveBlob,
  };
})();
