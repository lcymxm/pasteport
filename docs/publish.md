# 发布到 GitHub

这份清单是给仓库维护者看的。第一次发布照着做一遍，大约 10 分钟。

## 1. 建一个空仓库

在 GitHub 上 New repository：

- 名字：`pasteport`
- **不要**勾选 "Add a README"、"Add .gitignore"、"Choose a license" —— 仓库里已经有了

## 2. 推第一次

```bash
cd pasteport
git remote add origin https://github.com/<你的用户名>/pasteport.git
git push -u origin main
```

## 3. 打标签，让 CI 出两个产物

```bash
git tag v0.1.0
git push origin v0.1.0
```

GitHub Actions 会在 `windows-latest` 上：

1. 装 Go、JDK 21
2. 下载 `platform-34` + `build-tools;36.0.0`（`scripts/setup-android-sdk.ps1`）
3. 编出 `PastePort.exe` 和 `PastePort.apk`
4. 建一个 Release，把两个文件挂上去

想先验证再发版？推 `main` 分支也会跑同样的构建，产物在 Actions 页面的 Artifacts 里。

## 4. 换掉图片里的占位地址

`docs/social-preview.png` 左下角写着 `github.com/<you>/pasteport`。重新生成：

```bash
pip install pillow numpy
python scripts/make-assets.py
```

图标、社交预览、APK 的五种密度图标会一起重新生成。

## 5. 让仓库更容易被搜到

这一步比写代码更能决定有没有人点进来：

| 位置 | 建议 |
|---|---|
| **About → Description** | `电脑复制，手机粘贴 —— Windows ⇄ Android 局域网剪贴板与文件直传，无云无账号` |
| **Topics** | `clipboard` `clipboard-sync` `lan` `file-transfer` `windows` `android` `go` `self-hosted` `no-cloud` `qr-code` `privacy` |
| **Social preview** | 上传 `docs/social-preview.png`（Settings → Social preview） |
| **Release notes** | 每次写清楚改了什么、怎么升级 |

README 的第一屏（图标 + 一句话 + 那张预览图）决定 90% 的跳出率，改文案时优先动那里。

## 6. 可选：固定 APK 签名

CI 默认每次生成新的调试密钥，签出来的 APK 不能覆盖安装旧版本。想固定的话：

```bash
# 生成 keystore
keytool -genkeypair -keystore release.keystore -alias pasteport \
  -keyalg RSA -keysize 2048 -validity 10000 -storepass <密码> -dname "CN=PastePort"

# 转成 base64 存进 GitHub Actions Secret：ANDROID_KEYSTORE_BASE64
base64 -w0 release.keystore
```

工作流检测到 `ANDROID_KEYSTORE_BASE64` 就会用它签名。**别把 keystore 提交进仓库。**

## 7. 日常发版

```bash
git commit -am "fix: ..."
git push
git tag v0.1.1 && git push origin v0.1.1
```

版本号只有两处来源：

- `internal/app/server.go` 里的 `Version` 常量（显示在网页页脚和 `/api/info`）
- `android/AndroidManifest.xml` 里的 `versionName` / `versionCode`（`build-apk.ps1` 直接从这里读，不用另改脚本）
