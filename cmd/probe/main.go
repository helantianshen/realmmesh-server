package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	probev1 "github.com/helantianshen/realmmesh-server/gen/probe/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:9000", "gRPC 服务地址")
	message := flag.String("message", "Hello RealmMesh", "回显消息")
	flag.Parse()

	if err := run(*addr, *message); err != nil {
		log.Fatal(err)
	}
}

func run(addr, message string) error {
	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
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
