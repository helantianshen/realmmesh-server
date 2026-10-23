// 仓库传输集成验证：在显式测试连接的独立 schema 中运行真实 HTTP 与内存 gRPC 链路
// 不读取日常配置，不监听系统端口，测试结束只清理本次创建的 schema

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	warehousev1 "github.com/helantianshen/realmmesh-server/gen/warehouse/v1"
	"github.com/helantianshen/realmmesh-server/internal/biz"
	"github.com/helantianshen/realmmesh-server/internal/config"
	"github.com/helantianshen/realmmesh-server/internal/data"
	"github.com/helantianshen/realmmesh-server/internal/handler"
	"github.com/helantianshen/realmmesh-server/internal/itemdata"
	"github.com/helantianshen/realmmesh-server/internal/model"
	"github.com/helantianshen/realmmesh-server/internal/repo"
	"github.com/helantianshen/realmmesh-server/pkg/idgen"
	"github.com/helantianshen/realmmesh-server/pkg/serversecret"
	"github.com/helantianshen/realmmesh-server/pkg/token"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type warehouseRPCFixture struct {
	db         *gorm.DB
	transports *Transports
	client     warehousev1.WarehouseServiceClient
	tokens     *token.Manager
	secrets    map[int64]string
}

// TestWarehouseTransportPostgres 验证公开协议、身份边界和库存事务的完整中心链路
func TestWarehouseTransportPostgres(t *testing.T) {
	fixture := openWarehouseRPCFixture(t)
	t.Run("rpc_workflow_and_identity", fixture.workflow)
	t.Run("policy_http_and_matching", fixture.policies)
	t.Run("deleted_account_settlement", fixture.deletedAccount)
}

// openWarehouseRPCFixture 只使用显式测试 DSN，所有迁移和测试写入限定在随机 schema
func openWarehouseRPCFixture(t *testing.T) *warehouseRPCFixture {
	t.Helper()
	dsn := os.Getenv("REALMMESH_WAREHOUSE_TEST_DSN")
	if dsn == "" {
		t.Skip("未设置 REALMMESH_WAREHOUSE_TEST_DSN，未连接数据库")
	}
	configuration, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal("测试 DSN 格式无效")
	}
	adminPool := stdlib.OpenDB(*configuration)
	t.Cleanup(func() { _ = adminPool.Close() })
	if err := adminPool.PingContext(t.Context()); err != nil {
		t.Fatal("无法连接指定的仓库测试数据库")
	}
	schema := "warehouse_rpc_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := adminPool.ExecContext(t.Context(), "CREATE SCHEMA "+schema); err != nil {
		t.Fatal("无法创建仓库协议测试 schema")
	}
	t.Cleanup(func() {
		if _, err := adminPool.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error("清理仓库协议测试 schema 失败")
		}
	})
	configuration.RuntimeParams["search_path"] = schema
	configuration.RuntimeParams["statement_timeout"] = "10000"
	pool := stdlib.OpenDB(*configuration)
	pool.SetMaxOpenConns(8)
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{SkipDefaultTransaction: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("无法初始化仓库协议测试连接")
	}
	ids, err := idgen.New(config.IDConfig{NodeID: 1023})
	if err != nil {
		t.Fatal(err)
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.AutoMigrate(&model.User{}, &model.Server{}, &model.SystemConfig{}, &model.BusinessOperationEvent{}, &model.WarehouseItem{}, &model.WarehouseOperation{}, &model.WarehouseEvent{}); err != nil {
			return err
		}
		if err := data.MigrateWarehouseConstraints(tx); err != nil {
			return err
		}
		return data.InitSystemConfig(t.Context(), tx, ids)
	})
	if err != nil {
		t.Fatalf("迁移仓库协议测试 schema: %v", err)
	}
	for _, user := range []model.User{
		{ID: 1001, PlayerName: "Alice", PasswordHash: "unused-in-warehouse-test"},
		{ID: 1002, PlayerName: "Bob", PasswordHash: "unused-in-warehouse-test"},
		{ID: 1003, PlayerName: "Admin", PasswordHash: "unused-in-warehouse-test", IsAdmin: 1},
		{ID: 1004, PlayerName: "DeletedPlayer", PasswordHash: "unused-in-warehouse-test"},
	} {
		if err := db.Create(&user).Error; err != nil {
			t.Fatal(err)
		}
	}
	fixture := &warehouseRPCFixture{db: db, secrets: make(map[int64]string)}
	for _, serverID := range []int64{2001, 2002} {
		plain, digest := serversecret.Generate()
		fixture.secrets[serverID] = plain
		if err := db.Create(&model.Server{ID: serverID, ServerName: "isolated-warehouse", SecretHash: digest, Enabled: true}).Error; err != nil {
			t.Fatal(err)
		}
	}
	fixture.tokens, err = token.New(strings.Repeat("w", 32), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	businesses := biz.NewBusinesses(repo.NewRepositories(db), ids)
	handlers := handler.NewHandlers(businesses, fixture.tokens)
	fixture.transports, err = NewTransports(config.ServerConfig{HTTP: config.HTTPConfig{Addr: "127.0.0.1:8000"}, GRPC: config.GRPCConfig{Addr: "127.0.0.1:9000"}}, handlers, businesses, fixture.tokens)
	if err != nil {
		t.Fatal(err)
	}
	listener := bufconn.Listen(2 << 20)
	serveResult := make(chan error, 1)
	go func() { serveResult <- fixture.transports.GRPC.Serve(listener) }()
	t.Cleanup(func() {
		fixture.transports.GRPC.Server.Stop()
		_ = listener.Close()
		if err := <-serveResult; err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			t.Error(err)
		}
	})
	connection, err := grpc.NewClient("passthrough:///warehouse-integration", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	fixture.client = warehousev1.NewWarehouseServiceClient(connection)
	return fixture
}

// rpcContext 将每次真实 RPC 交给统一服务器凭据拦截器校验
func (fixture *warehouseRPCFixture) rpcContext(t *testing.T, serverID int64) context.Context {
	return metadata.NewOutgoingContext(t.Context(), metadata.Pairs("x-realmmesh-server-id", strconv.FormatInt(serverID, 10), "authorization", "Bearer "+fixture.secrets[serverID]))
}

// depositInput 使用真实 NeoForge 导出载荷构造当前账户的独立操作
func depositInput(t *testing.T, fixtureName string, quantity int64) *warehousev1.DepositRequest {
	t.Helper()
	payload, err := os.ReadFile("../itemdata/testdata/neoforge-1.21.1/" + fixtureName + ".nbt")
	if err != nil {
		t.Fatal(err)
	}
	itemID, stackable := "minecraft:stone", true
	if fixtureName == "damaged" {
		itemID, stackable = "minecraft:diamond_sword", false
	}
	return &warehousev1.DepositRequest{Player: &warehousev1.PlayerIdentity{UserId: 1001, PlayerName: "Alice"}, OperationId: uuid.NewString(), ItemId: itemID, ItemData: payload, Stackable: stackable, Quantity: quantity}
}

func requireRPCCode(t *testing.T, err error, expected codes.Code) {
	t.Helper()
	if status.Code(err) != expected {
		t.Fatalf("RPC 状态 %s，期望 %s: %v", status.Code(err), expected, err)
	}
}

func (fixture *warehouseRPCFixture) quantity(t *testing.T, inventoryID int64) int64 {
	t.Helper()
	var item model.WarehouseItem
	if err := fixture.db.First(&item, inventoryID).Error; err != nil {
		t.Fatal(err)
	}
	return item.Quantity
}

// httpJSON 使用真实路由和 JWT 中间件处理内存请求，不创建网络监听
func (fixture *warehouseRPCFixture) httpJSON(t *testing.T, actorID int64, path, body string, expected int) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if actorID != 0 {
		issued, err := fixture.tokens.Issue(actorID)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+issued.Value)
	}
	response := httptest.NewRecorder()
	fixture.transports.HTTP.ServeHTTP(response, request)
	if response.Code != expected {
		t.Fatalf("%s HTTP %d，期望 %d: %s", path, response.Code, expected, response.Body.String())
	}
	return response
}

func (fixture *warehouseRPCFixture) setPolicy(t *testing.T, policy itemdata.Policy) {
	t.Helper()
	encoded, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	fixture.httpJSON(t, 1003, "/api/v1/admin/warehouse/set-policy", string(encoded), http.StatusOK)
}

// workflow 通过完整 RPC 链验证预查不扣库、提交后结算、去重与账户和服务器隔离
func (fixture *warehouseRPCFixture) workflow(t *testing.T) {
	ctx := fixture.rpcContext(t, 2001)
	deposit := depositInput(t, "stone", 64)
	_, err := fixture.client.Deposit(t.Context(), deposit)
	requireRPCCode(t, err, codes.Unauthenticated)
	operation, err := fixture.client.Deposit(ctx, deposit)
	requireRPCCode(t, err, codes.OK)
	if operation.GetState() != warehousev1.OperationState_OPERATION_STATE_DEPOSITED {
		t.Fatal("存入未提交")
	}
	repeated, err := fixture.client.Deposit(ctx, deposit)
	requireRPCCode(t, err, codes.OK)
	if repeated.GetInventoryId() != operation.GetInventoryId() || fixture.quantity(t, operation.InventoryId) != 64 {
		t.Fatal("重复存入改变库存")
	}
	deposit.Quantity = 32
	_, err = fixture.client.Deposit(ctx, deposit)
	requireRPCCode(t, err, codes.AlreadyExists)
	deposit.Quantity = 64
	_, err = fixture.client.Deposit(fixture.rpcContext(t, 2002), deposit)
	requireRPCCode(t, err, codes.AlreadyExists)
	items, err := fixture.client.ListItems(ctx, &warehousev1.ListItemsRequest{Player: deposit.Player})
	requireRPCCode(t, err, codes.OK)
	if len(items.Items) != 1 || items.Items[0].Quantity != 64 || len(items.Items[0].ItemData) != 0 {
		t.Fatal("列表未返回正确摘要")
	}
	wrongPlayer := &warehousev1.PlayerIdentity{UserId: 1001, PlayerName: "Bob"}
	_, err = fixture.client.ListItems(ctx, &warehousev1.ListItemsRequest{Player: wrongPlayer})
	requireRPCCode(t, err, codes.PermissionDenied)

	withdraw := &warehousev1.WithdrawalRequest{Player: deposit.Player, OperationId: uuid.NewString(), InventoryId: operation.InventoryId, Quantity: 20}
	prepared, err := fixture.client.PrepareWithdrawal(ctx, withdraw)
	requireRPCCode(t, err, codes.OK)
	if fixture.quantity(t, operation.InventoryId) != 64 || len(prepared.GetItem().GetItemData()) == 0 {
		t.Fatal("预查改变库存或缺少载荷")
	}
	_, err = fixture.client.GetOperation(ctx, &warehousev1.GetOperationRequest{Player: deposit.Player, OperationId: withdraw.OperationId})
	requireRPCCode(t, err, codes.NotFound)
	commit := &warehousev1.CommitWithdrawalRequest{Request: withdraw, ItemDataHash: prepared.Item.ItemDataHash}
	committed, err := fixture.client.CommitWithdrawal(ctx, commit)
	requireRPCCode(t, err, codes.OK)
	if committed.State != warehousev1.OperationState_OPERATION_STATE_DEBITED || fixture.quantity(t, operation.InventoryId) != 44 {
		t.Fatal("扣库结果不符")
	}
	_, err = fixture.client.CommitWithdrawal(ctx, commit)
	requireRPCCode(t, err, codes.OK)
	if fixture.quantity(t, operation.InventoryId) != 44 {
		t.Fatal("重复提交再次扣库")
	}
	complete := &warehousev1.CompleteWithdrawalRequest{Player: deposit.Player, OperationId: withdraw.OperationId, Delivered: true}
	finished, err := fixture.client.CompleteWithdrawal(ctx, complete)
	requireRPCCode(t, err, codes.OK)
	if finished.State != warehousev1.OperationState_OPERATION_STATE_DELIVERED {
		t.Fatal("完成状态不符")
	}
	_, err = fixture.client.CompleteWithdrawal(ctx, complete)
	requireRPCCode(t, err, codes.OK)
	complete.Delivered, complete.Reason = false, "inventory_full"
	_, err = fixture.client.CompleteWithdrawal(ctx, complete)
	requireRPCCode(t, err, codes.AlreadyExists)
	_, err = fixture.client.GetOperation(fixture.rpcContext(t, 2002), &warehousev1.GetOperationRequest{Player: deposit.Player, OperationId: withdraw.OperationId})
	requireRPCCode(t, err, codes.NotFound)

	withdraw.OperationId, withdraw.Quantity = uuid.NewString(), 10
	prepared, err = fixture.client.PrepareWithdrawal(ctx, withdraw)
	requireRPCCode(t, err, codes.OK)
	commit.ItemDataHash = prepared.Item.ItemDataHash
	_, err = fixture.client.CommitWithdrawal(ctx, commit)
	requireRPCCode(t, err, codes.OK)
	complete.OperationId = withdraw.OperationId
	finished, err = fixture.client.CompleteWithdrawal(ctx, complete)
	requireRPCCode(t, err, codes.OK)
	if finished.State != warehousev1.OperationState_OPERATION_STATE_REFUNDED || fixture.quantity(t, operation.InventoryId) != 44 {
		t.Fatal("明确未发放没有整笔退库")
	}
	_, err = fixture.client.CompleteWithdrawal(ctx, complete)
	requireRPCCode(t, err, codes.OK)
	if fixture.quantity(t, operation.InventoryId) != 44 {
		t.Fatal("重复失败确认重复退款")
	}
	state, err := fixture.client.GetOperation(ctx, &warehousev1.GetOperationRequest{Player: deposit.Player, OperationId: withdraw.OperationId})
	requireRPCCode(t, err, codes.OK)
	if state.State != warehousev1.OperationState_OPERATION_STATE_REFUNDED {
		t.Fatal("查询丢失终态")
	}
	withdraw.Player = &warehousev1.PlayerIdentity{UserId: 1002, PlayerName: "Bob"}
	_, err = fixture.client.PrepareWithdrawal(ctx, withdraw)
	requireRPCCode(t, err, codes.NotFound)

	for _, bad := range []*warehousev1.DepositRequest{
		{Player: deposit.Player, OperationId: uuid.NewString(), ItemId: "minecraft:stone", ItemData: []byte{10}, Stackable: true, Quantity: 1},
		{Player: deposit.Player, OperationId: uuid.NewString(), ItemId: "minecraft:dirt", ItemData: deposit.ItemData, Stackable: true, Quantity: 1},
		{Player: deposit.Player, OperationId: uuid.NewString(), ItemId: "minecraft:stone", ItemData: deposit.ItemData, Stackable: true},
		{Player: deposit.Player, OperationId: uuid.NewString(), ItemId: "minecraft:stone", ItemData: deposit.ItemData, Quantity: 2},
	} {
		_, err := fixture.client.Deposit(ctx, bad)
		requireRPCCode(t, err, codes.InvalidArgument)
	}
	if err := fixture.db.Model(&model.Server{}).Where("id = ?", 2002).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	_, err = fixture.client.ListItems(fixture.rpcContext(t, 2002), &warehousev1.ListItemsRequest{Player: deposit.Player})
	requireRPCCode(t, err, codes.Unauthenticated)
	if err := fixture.db.Model(&model.Server{}).Where("id = ?", 2002).Update("enabled", true).Error; err != nil {
		t.Fatal(err)
	}
}

// policies 通过管理员 HTTP 配置驱动实际入库判定，覆盖三种规则形式与黑白优先级
func (fixture *warehouseRPCFixture) policies(t *testing.T) {
	fixture.httpJSON(t, 0, "/api/v1/admin/warehouse/policy", "{}", http.StatusUnauthorized)
	fixture.httpJSON(t, 1001, "/api/v1/admin/warehouse/policy", "{}", http.StatusForbidden)
	fixture.httpJSON(t, 1003, "/api/v1/admin/warehouse/policy", "{}", http.StatusOK)
	fixture.httpJSON(t, 1001, "/api/v1/admin/warehouse/set-policy", "{}", http.StatusForbidden)
	fixture.httpJSON(t, 1003, "/api/v1/admin/warehouse/set-policy", `{"blacklist":[{}]}`, http.StatusBadRequest)
	fixture.httpJSON(t, 1003, "/api/v1/admin/warehouse/set-policy", `{"unknown":true}`, http.StatusBadRequest)
	fixture.httpJSON(t, 1003, "/api/v1/admin/warehouse/set-policy", "null", http.StatusBadRequest)
	// 持久化策略损坏不能退回默认允许，管理员仍能用完整策略修正配置
	if err := fixture.db.Model(&model.SystemConfig{}).Where("key = ?", model.ConfigWarehousePolicy).Update("value", "null").Error; err != nil {
		t.Fatal(err)
	}
	_, invalidPolicyErr := fixture.client.Deposit(fixture.rpcContext(t, 2001), depositInput(t, "stone", 1))
	requireRPCCode(t, invalidPolicyErr, codes.Internal)
	fixture.setPolicy(t, itemdata.Policy{})
	key := func(value string) itemdata.PathPart { return itemdata.PathPart{Key: &value} }
	nameExists := itemdata.Condition{Path: []itemdata.PathPart{key("components"), key("minecraft:custom_name")}, Operator: itemdata.OperatorExists}
	damageEquals := itemdata.Condition{Path: []itemdata.PathPart{key("components"), key("minecraft:damage")}, Operator: itemdata.OperatorTypedEquals, Type: "int", Value: []byte("7")}
	stone := itemdata.Rule{ItemID: "minecraft:stone"}
	for _, test := range []struct {
		name        string
		policy      itemdata.Policy
		fixtureName string
		expected    codes.Code
	}{
		{"id_only", itemdata.Policy{Blacklist: []itemdata.Rule{stone}}, "stone", codes.FailedPrecondition},
		{"id_ignores_components", itemdata.Policy{Blacklist: []itemdata.Rule{stone}}, "named", codes.FailedPrecondition},
		{"nbt_only", itemdata.Policy{Blacklist: []itemdata.Rule{{Conditions: []itemdata.Condition{nameExists}}}}, "named", codes.FailedPrecondition},
		{"missing_nbt", itemdata.Policy{Blacklist: []itemdata.Rule{{Conditions: []itemdata.Condition{nameExists}}}}, "stone", codes.OK},
		{"id_and_nbt", itemdata.Policy{Blacklist: []itemdata.Rule{{ItemID: "minecraft:diamond_sword", Conditions: []itemdata.Condition{damageEquals}}}}, "damaged", codes.FailedPrecondition},
		{"combined_mismatch", itemdata.Policy{Blacklist: []itemdata.Rule{{ItemID: "minecraft:stone", Conditions: []itemdata.Condition{damageEquals}}}}, "damaged", codes.OK},
		{"empty_whitelist", itemdata.Policy{OnlyWhitelist: true}, "stone", codes.FailedPrecondition},
		{"matching_whitelist", itemdata.Policy{OnlyWhitelist: true, Whitelist: []itemdata.Rule{stone}}, "stone", codes.OK},
		{"blacklist_first", itemdata.Policy{OnlyWhitelist: true, Whitelist: []itemdata.Rule{stone}, Blacklist: []itemdata.Rule{stone}}, "stone", codes.FailedPrecondition},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture.setPolicy(t, test.policy)
			request := depositInput(t, test.fixtureName, 1)
			_, err := fixture.client.Deposit(fixture.rpcContext(t, 2001), request)
			requireRPCCode(t, err, test.expected)
		})
	}
	// 策略上限以 64 KiB 为准，有效的大名单必须能通过管理入口完整保存
	large := itemdata.Policy{}
	for index := 0; index < 120; index++ {
		large.Blacklist = append(large.Blacklist, itemdata.Rule{ItemID: fmt.Sprintf("example:unmatched_item_%d", index)})
	}
	encoded, err := json.Marshal(large)
	if err != nil || len(encoded) <= 4096 {
		t.Fatal("大名单测试没有覆盖 HTTP 边界", err)
	}
	fixture.setPolicy(t, large)
	fixture.setPolicy(t, itemdata.Policy{})
	// 明确拒绝也是原操作的稳定结果，名单放开后重发不能变成重新入库
	denied := depositInput(t, "named", 1)
	fixture.setPolicy(t, itemdata.Policy{Blacklist: []itemdata.Rule{stone}})
	_, err = fixture.client.Deposit(fixture.rpcContext(t, 2001), denied)
	requireRPCCode(t, err, codes.FailedPrecondition)
	fixture.setPolicy(t, itemdata.Policy{})
	_, err = fixture.client.Deposit(fixture.rpcContext(t, 2001), denied)
	requireRPCCode(t, err, codes.FailedPrecondition)
	rejected, err := fixture.client.GetOperation(fixture.rpcContext(t, 2001), &warehousev1.GetOperationRequest{Player: denied.Player, OperationId: denied.OperationId})
	requireRPCCode(t, err, codes.OK)
	if rejected.State != warehousev1.OperationState_OPERATION_STATE_REJECTED {
		t.Fatal("拒绝终态未持久化")
	}
	// 已入库的操作不因后续名单变化变为失败，既有库存也仍可取出
	request := depositInput(t, "stone", 1)
	operation, err := fixture.client.Deposit(fixture.rpcContext(t, 2001), request)
	requireRPCCode(t, err, codes.OK)
	fixture.setPolicy(t, itemdata.Policy{Blacklist: []itemdata.Rule{stone}})
	_, err = fixture.client.Deposit(fixture.rpcContext(t, 2001), request)
	requireRPCCode(t, err, codes.OK)
	withdraw := &warehousev1.WithdrawalRequest{Player: request.Player, OperationId: uuid.NewString(), InventoryId: operation.InventoryId, Quantity: 1}
	prepared, err := fixture.client.PrepareWithdrawal(fixture.rpcContext(t, 2001), withdraw)
	requireRPCCode(t, err, codes.OK)
	_, err = fixture.client.CommitWithdrawal(fixture.rpcContext(t, 2001), &warehousev1.CommitWithdrawalRequest{Request: withdraw, ItemDataHash: prepared.Item.ItemDataHash})
	requireRPCCode(t, err, codes.OK)
	_, err = fixture.client.CompleteWithdrawal(fixture.rpcContext(t, 2001), &warehousev1.CompleteWithdrawalRequest{Player: request.Player, OperationId: withdraw.OperationId, Delivered: true})
	requireRPCCode(t, err, codes.OK)
	fixture.setPolicy(t, itemdata.Policy{})
}

// deletedAccount 确认删除账户后不能新增资产操作，但原可信服务器仍可结算历史未发放
func (fixture *warehouseRPCFixture) deletedAccount(t *testing.T) {
	fixture.setPolicy(t, itemdata.Policy{})
	request := depositInput(t, "stone", 8)
	request.Player = &warehousev1.PlayerIdentity{UserId: 1004, PlayerName: "DeletedPlayer"}
	ctx := fixture.rpcContext(t, 2001)
	operation, err := fixture.client.Deposit(ctx, request)
	requireRPCCode(t, err, codes.OK)
	withdraw := &warehousev1.WithdrawalRequest{Player: request.Player, OperationId: uuid.NewString(), InventoryId: operation.InventoryId, Quantity: 3}
	prepared, err := fixture.client.PrepareWithdrawal(ctx, withdraw)
	requireRPCCode(t, err, codes.OK)
	_, err = fixture.client.CommitWithdrawal(ctx, &warehousev1.CommitWithdrawalRequest{Request: withdraw, ItemDataHash: prepared.Item.ItemDataHash})
	requireRPCCode(t, err, codes.OK)
	fixture.httpJSON(t, 1003, "/api/v1/admin/users/delete", `{"user_id":"1004"}`, http.StatusNoContent)
	request.OperationId = uuid.NewString()
	_, err = fixture.client.Deposit(ctx, request)
	requireRPCCode(t, err, codes.PermissionDenied)
	_, err = fixture.client.ListItems(ctx, &warehousev1.ListItemsRequest{Player: request.Player})
	requireRPCCode(t, err, codes.PermissionDenied)
	complete := &warehousev1.CompleteWithdrawalRequest{Player: request.Player, OperationId: withdraw.OperationId, Reason: "player_unavailable"}
	_, err = fixture.client.CompleteWithdrawal(fixture.rpcContext(t, 2002), complete)
	requireRPCCode(t, err, codes.AlreadyExists)
	finished, err := fixture.client.CompleteWithdrawal(ctx, complete)
	requireRPCCode(t, err, codes.OK)
	if finished.State != warehousev1.OperationState_OPERATION_STATE_REFUNDED || fixture.quantity(t, operation.InventoryId) != 8 {
		t.Fatal("删除账户后的明确未发放没有退库")
	}
	result, err := fixture.client.GetOperation(ctx, &warehousev1.GetOperationRequest{Player: request.Player, OperationId: withdraw.OperationId})
	requireRPCCode(t, err, codes.OK)
	if result.State != warehousev1.OperationState_OPERATION_STATE_REFUNDED || !bytes.Equal(result.ItemDataHash, operation.ItemDataHash) {
		t.Fatal("历史结算查询不一致")
	}
}
