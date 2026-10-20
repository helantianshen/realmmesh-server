// 运维诊断：探测命令错误
// 定义启动探测前缺少有效服务器凭证的固定错误

package main

import "errors"

var (
	ErrProbeCredentials = errors.New("需要正的 -server-id 以及 REALMMESH_SERVER_SECRET 环境变量中的服务器密钥")
)
