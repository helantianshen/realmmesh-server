package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	probev1 "github.com/helantianshen/realmmesh-server/gen/probe/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// main 解析探测参数并报告执行结果
func main() {
	addr := flag.String("addr", "127.0.0.1:9000", "gRPC 服务地址")
	message := flag.String("message", "Hello RealmMesh", "回显消息")
	serverID := flag.Int64("server-id", 0, "中心登记的服务器 ID")
	flag.Parse()

	if err := run(*addr, *message, *serverID, os.Getenv("REALMMESH_SERVER_SECRET")); err != nil {
		log.Fatal(err)
	}
}

// run 调用 Echo 并核对响应，验证完成后关闭客户端连接
func run(addr, message string, serverID int64, secret string) error {
	if serverID <= 0 || len(secret) != 43 {
		return ErrProbeCredentials
	}

	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithPerRPCCredentials(serverCredentials{id: serverID, secret: secret}),
	)
	if err != nil {
		return fmt.Errorf("创建 gRPC 客户端: %w", err)
	}
	defer conn.Close()

	client := probev1.NewProbeServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	response, err := client.Echo(ctx, &probev1.EchoRequest{
		Message: message,
	})
	if err != nil {
		return fmt.Errorf("调用 Echo: %w", err)
	}

	if response.GetMessage() != message {
		return fmt.Errorf(
			"Echo 响应不一致，发送 %q，收到 %q",
			message,
			response.GetMessage(),
		)
	}

	fmt.Printf("Echo 验证通过：%q\n", response.GetMessage())
	return nil
}

// serverCredentials 向每次 RPC 附加服务器身份和原始密钥
type serverCredentials struct {
	id     int64
	secret string
}

// GetRequestMetadata 将公开 ID 转为十进制文本，密钥只进入请求 metadata
func (c serverCredentials) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return map[string]string{
		"x-realmmesh-server-id": strconv.FormatInt(c.id, 10),
		"authorization":         "Bearer " + c.secret,
	}, nil
}

// RequireTransportSecurity 允许在当前明文 gRPC 通道发送服务器凭据
func (serverCredentials) RequireTransportSecurity() bool {
	return false
}
