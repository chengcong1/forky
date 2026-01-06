# 安装用于授权的私钥程序
```
go install -ldflags="-w -s" github.com/chengcong1/forky/cmd/offauth
```
```
.\offauth.exe genkey

.\offauth.exe sign -keypath .\private_ed25519.key -days 3 -customer name -reqfile .\req.dat

```