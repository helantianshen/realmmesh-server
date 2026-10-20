package main

import "errors"

var (
	ErrProbeCredentials = errors.New("需要正的 -server-id 以及 REALMMESH_SERVER_SECRET 环境变量中的服务器密钥")
)
