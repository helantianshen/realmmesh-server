// 仓库压力基准：显式启用后，通过回环 TCP gRPC 测量真实数据库链路
// 复用隔离 schema 夹具，负载停止后核对库存、操作及审计，不修改业务实现

package server

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net"
	"os"
	"runtime"
	"slices"
	"sync"
	"syscall"
	"testing"
	"time"

	"errors"
	"github.com/google/uuid"
	warehousev1 "github.com/helantianshen/realmmesh-server/gen/warehouse/v1"
	"github.com/helantianshen/realmmesh-server/internal/itemdata"
	"github.com/helantianshen/realmmesh-server/internal/model"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

type warehouseLoadResult struct {
	Scenario                 string         `json:"scenario"`
	Round                    int            `json:"round"`
	Concurrency              int            `json:"concurrency"`
	PayloadBytes             int            `json:"payload_bytes"`
	Attempts                 int            `json:"attempts"`
	Successes                int            `json:"successes"`
	Errors                   map[string]int `json:"errors"`
	WarmErrors               map[string]int `json:"warm_errors"`
	ElapsedSeconds           float64        `json:"elapsed_seconds"`
	OperationsPerSecond      float64        `json:"operations_per_second"`
	P50MS                    float64        `json:"p50_ms"`
	P95MS                    float64        `json:"p95_ms"`
	P99MS                    float64        `json:"p99_ms"`
	AllocBytesPerOperation   uint64         `json:"alloc_bytes_per_operation"`
	ProcessCPUSeconds        float64        `json:"process_cpu_seconds"`
	DBWaitCount              int64          `json:"db_wait_count"`
	DBWaitSeconds            float64        `json:"db_wait_seconds"`
	Consistent               bool           `json:"consistent"`
	FailedOperationStates    map[string]int `json:"failed_operation_states"`
	UnconfirmedDebits        int64          `json:"unconfirmed_debits"`
	BackendDrainSeconds      float64        `json:"backend_drain_seconds"`
	AuditStructureChecked    bool           `json:"audit_structure_checked"`
	AuditStructureMismatches int64          `json:"audit_structure_mismatches"`
}

// TestWarehouseLoadPostgres 使用两个复用的 TCP 连接生成闭环负载，默认跳过
// 必须同时指定测试 DSN、输出目录和启用标志；输出包含全部尝试的延迟及状态
func TestWarehouseLoadPostgres(t *testing.T) {
	if os.Getenv("REALMMESH_WAREHOUSE_LOAD") != "1" {
		t.Skip("未启用仓库压力基准")
	}
	output := os.Getenv("REALMMESH_WAREHOUSE_LOAD_OUTPUT")
	if output == "" || os.Getenv("REALMMESH_WAREHOUSE_TEST_DSN") == "" {
		t.Fatal("压力基准必须指定输出目录和测试 DSN")
	}
	logFile, err := os.OpenFile(output+"/rpc.log", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logFile, nil)))
	defer slog.SetDefault(oldLogger)
	fixture := openWarehouseRPCFixture(t)
	pool, err := fixture.db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(10)
	pool.SetMaxIdleConns(10)
	pool.SetConnMaxLifetime(30 * time.Minute)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- fixture.transports.GRPC.Serve(listener) }()
	t.Cleanup(func() { _ = listener.Close(); <-serveDone })
	clients := make([]warehousev1.WarehouseServiceClient, 2)
	for i := range clients {
		conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		clients[i] = warehousev1.NewWarehouseServiceClient(conn)
	}
	players := make([]*warehousev1.PlayerIdentity, 64)
	for i := range players {
		players[i] = &warehousev1.PlayerIdentity{UserId: int64(10000 + i), PlayerName: fmt.Sprintf("Load%03d", i)}
		if err := fixture.db.Create(&model.User{ID: players[i].UserId, PlayerName: players[i].PlayerName, PasswordHash: "unused-load-test"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	duration := 3 * time.Second
	if value := os.Getenv("REALMMESH_WAREHOUSE_LOAD_DURATION"); value != "" {
		duration, err = time.ParseDuration(value)
		if err != nil || duration < time.Second || duration > time.Minute {
			t.Fatal("基准持续时间必须在 1s 到 1m 之间")
		}
	}
	var schema string
	fixture.db.Raw("SELECT current_schema()").Scan(&schema)
	results := []warehouseLoadResult{}
	// 结果先写入输出目录；schema 删除由复用夹具负责，运行器另行核对清理结果
	t.Cleanup(func() {
		encoded, _ := json.MarshalIndent(results, "", "  ")
		if err := os.WriteFile(output+"/results.json", append(encoded, '\n'), 0600); err != nil {
			t.Error(err)
		}
	})
	for round := 1; round <= 2; round++ {
		for _, scenario := range []string{"deposit_hot", "deposit_distributed", "deposit_16k", "withdrawal_hot", "withdrawal_distributed", "list_50_of_100", "duplicate_deposit"} {
			for _, concurrency := range []int{1, 8, 32, 64} {
				t.Run(fmt.Sprintf("%s/c%d/r%d", scenario, concurrency, round), func(t *testing.T) {
					if err := fixture.db.Exec("TRUNCATE warehouse_items, warehouse_operations, warehouse_events").Error; err != nil {
						t.Fatal(err)
					}
					payload := depositInput(t, "stone", 1).ItemData
					if scenario == "deposit_16k" {
						payload = warehouseLoadPayload()
					}
					inventories := make([]int64, 64)
					seedQuantity := int64(0)
					if scenario == "withdrawal_hot" || scenario == "withdrawal_distributed" {
						count := 64
						if scenario == "withdrawal_hot" {
							count = 1
						}
						for i := 0; i < count; i++ {
							op, err := clients[0].Deposit(fixture.rpcContext(t, 2001), &warehousev1.DepositRequest{Player: players[i], OperationId: uuid.NewString(), ItemId: "minecraft:stone", ItemData: payload, Stackable: true, Quantity: 2304})
							if err != nil {
								t.Fatal(err)
							}
							inventories[i] = op.InventoryId
							seedQuantity += 2304
						}
						// 只扩大测试库存，补齐同事务审计数量，使限时负载不因正常缺货结束
						if err := fixture.db.Exec("UPDATE warehouse_items SET quantity=10000000").Error; err != nil {
							t.Fatal(err)
						}
						if err := fixture.db.Exec("UPDATE warehouse_operations SET quantity=10000000").Error; err != nil {
							t.Fatal(err)
						}
						if err := fixture.db.Exec("UPDATE warehouse_events SET quantity=10000000").Error; err != nil {
							t.Fatal(err)
						}
						seedQuantity = int64(count) * 10000000
					}
					if scenario == "list_50_of_100" {
						rows := make([]model.WarehouseItem, 0, 6400)
						document, err := itemdata.Parse("minecraft:diamond_sword", depositInput(t, "damaged", 1).ItemData)
						if err != nil {
							t.Fatal(err)
						}
						for _, player := range players {
							for n := 0; n < 100; n++ {
								rows = append(rows, model.WarehouseItem{ID: int64(len(rows) + 1), UserID: player.UserId, ItemID: document.ItemID, ItemData: document.Canonical, ItemDataHash: document.Hash[:], Quantity: 1, Stackable: false})
							}
						}
						if err := fixture.db.CreateInBatches(rows, 200).Error; err != nil {
							t.Fatal(err)
						}
						seedQuantity = 6400
					}
					duplicateID := uuid.NewString()
					failedIDs := make([][]string, concurrency)
					action := func(worker int) (resultErr error) {
						var attemptedID string
						defer func() {
							if resultErr != nil && attemptedID != "" {
								failedIDs[worker] = append(failedIDs[worker], attemptedID)
							}
						}()
						playerIndex := worker % 64
						if scenario == "deposit_hot" || scenario == "withdrawal_hot" || scenario == "duplicate_deposit" {
							playerIndex = 0
						}
						player := players[playerIndex]
						client := clients[worker%2]
						ctx, cancel := context.WithTimeout(fixture.rpcContext(t, int64(2001+worker%2)), 10*time.Second)
						defer cancel()
						if scenario == "duplicate_deposit" {
							client = clients[0]
							ctx = fixture.rpcContext(t, 2001)
							var stop context.CancelFunc
							ctx, stop = context.WithTimeout(ctx, 10*time.Second)
							defer stop()
						}
						switch scenario {
						case "withdrawal_hot", "withdrawal_distributed":
							request := &warehousev1.WithdrawalRequest{Player: player, OperationId: uuid.NewString(), InventoryId: inventories[playerIndex], Quantity: 1}
							attemptedID = request.OperationId
							prepared, err := client.PrepareWithdrawal(ctx, request)
							if err != nil {
								return err
							}
							committed, err := client.CommitWithdrawal(ctx, &warehousev1.CommitWithdrawalRequest{Request: request, ItemDataHash: prepared.Item.ItemDataHash})
							if err != nil {
								return err
							}
							if committed.State != warehousev1.OperationState_OPERATION_STATE_DEBITED {
								return fmt.Errorf("unexpected debit state")
							}
							finished, err := client.CompleteWithdrawal(ctx, &warehousev1.CompleteWithdrawalRequest{Player: player, OperationId: request.OperationId, Delivered: true})
							if err == nil && finished.State != warehousev1.OperationState_OPERATION_STATE_DELIVERED {
								return fmt.Errorf("unexpected completion state")
							}
							return err
						case "list_50_of_100":
							page, err := client.ListItems(ctx, &warehousev1.ListItemsRequest{Player: player, Limit: 50})
							if err != nil {
								return err
							}
							if len(page.Items) != 50 {
								return fmt.Errorf("unexpected page size")
							}
							for _, item := range page.Items {
								if len(item.ItemData) != 0 {
									return fmt.Errorf("list includes payload")
								}
							}
							return nil
						default:
							operationID := uuid.NewString()
							if scenario == "duplicate_deposit" {
								operationID = duplicateID
							}
							attemptedID = operationID
							op, err := client.Deposit(ctx, &warehousev1.DepositRequest{Player: player, OperationId: operationID, ItemId: "minecraft:stone", ItemData: payload, Stackable: true, Quantity: 1})
							if err == nil && op.State != warehousev1.OperationState_OPERATION_STATE_DEPOSITED {
								return fmt.Errorf("unexpected deposit state")
							}
							return err
						}
					}
					// 少量预热也计入最终库存预期，但不进入时间分位数
					warm := 0
					warmErrors := map[string]int{}
					for worker := 0; worker < concurrency; worker++ {
						if err := action(worker); err != nil {
							warmErrors[status.Code(err).String()]++
							continue
						}
						warm++
					}
					before := pool.Stats()
					var memBefore, memAfter runtime.MemStats
					runtime.ReadMemStats(&memBefore)
					var cpuBefore, cpuAfter syscall.Rusage
					_ = syscall.Getrusage(syscall.RUSAGE_SELF, &cpuBefore)
					type samples struct {
						latencies []time.Duration
						successes int
						errors    map[string]int
					}
					workers := make([]samples, concurrency)
					start := time.Now()
					end := start.Add(duration)
					var group sync.WaitGroup
					for worker := 0; worker < concurrency; worker++ {
						group.Add(1)
						go func() {
							defer group.Done()
							s := &workers[worker]
							s.errors = map[string]int{}
							for time.Now().Before(end) {
								began := time.Now()
								err := action(worker)
								s.latencies = append(s.latencies, time.Since(began))
								if err == nil {
									s.successes++
								} else {
									code := status.Code(err).String()
									if code == "Unknown" {
										code = "assertion_or_unknown"
									}
									s.errors[code]++
								}
							}
						}()
					}
					group.Wait()
					elapsed := time.Since(start)
					runtime.ReadMemStats(&memAfter)
					_ = syscall.Getrusage(syscall.RUSAGE_SELF, &cpuAfter)
					after := pool.Stats()
					result := warehouseLoadResult{Scenario: scenario, Round: round, Concurrency: concurrency, PayloadBytes: len(payload), Errors: map[string]int{}, WarmErrors: warmErrors, ElapsedSeconds: elapsed.Seconds(), DBWaitCount: after.WaitCount - before.WaitCount, DBWaitSeconds: (after.WaitDuration - before.WaitDuration).Seconds()}
					var latencies []time.Duration
					for _, s := range workers {
						latencies = append(latencies, s.latencies...)
						result.Successes += s.successes
						for code, count := range s.errors {
							result.Errors[code] += count
						}
					}
					slices.Sort(latencies)
					result.Attempts = len(latencies)
					if len(latencies) == 0 {
						t.Fatal("没有产生负载")
					}
					percentile := func(p float64) float64 {
						return float64(latencies[int(float64(len(latencies)-1)*p)]) / float64(time.Millisecond)
					}
					result.P50MS = percentile(.50)
					result.P95MS = percentile(.95)
					result.P99MS = percentile(.99)
					result.OperationsPerSecond = float64(result.Successes) / elapsed.Seconds()
					result.AllocBytesPerOperation = (memAfter.TotalAlloc - memBefore.TotalAlloc) / uint64(result.Attempts)
					cpu := func(r syscall.Rusage) float64 {
						return float64(r.Utime.Sec+r.Stime.Sec) + float64(r.Utime.Usec+r.Stime.Usec)/1e6
					}
					result.ProcessCPUSeconds = cpu(cpuAfter) - cpu(cpuBefore)
					// 客户端超时不等于未提交；等待后台数据库工作安静后，按失败操作 ID 读取事实
					drainStart := time.Now()
					quietSince := time.Now()
					for {
						if pool.Stats().InUse != 0 {
							quietSince = time.Now()
						}
						if time.Since(quietSince) >= 300*time.Millisecond {
							break
						}
						if time.Since(drainStart) > 10*time.Second {
							t.Fatal("数据库后台请求未收敛")
						}
						time.Sleep(25 * time.Millisecond)
					}
					result.BackendDrainSeconds = time.Since(drainStart).Seconds()
					result.FailedOperationStates = map[string]int{}
					seenFailed := map[string]bool{}
					unknownApplied := 0
					for _, ids := range failedIDs {
						for _, id := range ids {
							if seenFailed[id] {
								continue
							}
							seenFailed[id] = true
							var operation model.WarehouseOperation
							err := fixture.db.Where("operation_id = ?", id).First(&operation).Error
							if errors.Is(err, gorm.ErrRecordNotFound) {
								result.FailedOperationStates["not_persisted"]++
								continue
							}
							if err != nil {
								t.Fatal(err)
							}
							result.FailedOperationStates[operation.State]++
							switch operation.State {
							case model.WarehouseCommitted, model.WarehouseDebited, model.WarehouseDelivered:
								unknownApplied++
							default:
								t.Errorf("失败请求的状态异常: %s", operation.State)
							}
							if operation.State == model.WarehouseDebited {
								result.UnconfirmedDebits++
							}
						}
					}

					var quantity, unsettled, mismatches, operations, events int64
					if err := fixture.db.Raw("SELECT COALESCE(SUM(quantity),0) FROM warehouse_items").Scan(&quantity).Error; err != nil {
						t.Fatal(err)
					}
					if err := fixture.db.Model(&model.WarehouseOperation{}).Where("state = ?", model.WarehouseDebited).Count(&unsettled).Error; err != nil {
						t.Fatal(err)
					}
					if err := fixture.db.Model(&model.WarehouseOperation{}).Count(&operations).Error; err != nil {
						t.Fatal(err)
					}
					if err := fixture.db.Model(&model.WarehouseEvent{}).Count(&events).Error; err != nil {
						t.Fatal(err)
					}
					expected := seedQuantity + int64(warm+result.Successes+unknownApplied)
					if scenario == "withdrawal_hot" || scenario == "withdrawal_distributed" {
						expected = seedQuantity - int64(warm+result.Successes+unknownApplied)
					}
					if scenario == "list_50_of_100" {
						expected = seedQuantity
					} else {
						if err := fixture.db.Raw(`SELECT count(*) FROM warehouse_items i FULL JOIN
       (SELECT inventory_id, sum(CASE state WHEN 'committed' THEN quantity WHEN 'debited' THEN -quantity WHEN 'refunded' THEN quantity ELSE 0 END) AS quantity FROM warehouse_events GROUP BY inventory_id) e
       ON i.id=e.inventory_id WHERE i.id IS NULL OR e.inventory_id IS NULL OR i.quantity<>e.quantity`).Scan(&mismatches).Error; err != nil {
							t.Fatal(err)
						}
					}
					if scenario == "duplicate_deposit" {
						expected = 1
						if operations != 1 || events != 1 {
							mismatches++
						}
					}
					if scenario == "deposit_hot" || scenario == "deposit_distributed" || scenario == "deposit_16k" {
						if operations != int64(warm+result.Successes+unknownApplied) || events != operations {
							mismatches++
						}
					}
					// 净额无法发现重复或缺失的 delivered 事件，需逐笔检查完整状态迁移与事件归属
					if err := fixture.db.Raw(`WITH audit AS (
                        SELECT operation_id, count(*) AS total,
                        count(*) FILTER (WHERE state='committed') AS committed,
                        count(*) FILTER (WHERE state='rejected') AS rejected,
                        count(*) FILTER (WHERE state='debited') AS debited,
                        count(*) FILTER (WHERE state='delivered') AS delivered,
                        count(*) FILTER (WHERE state='refunded') AS refunded
                        FROM warehouse_events GROUP BY operation_id
                    ) SELECT count(*) FROM warehouse_operations o FULL JOIN audit a USING (operation_id)
                    WHERE o.id IS NULL
                    OR coalesce(a.total,0) <> CASE WHEN o.state IN ('delivered','refunded') THEN 2 ELSE 1 END
                    OR a.committed <> CASE WHEN o.state='committed' THEN 1 ELSE 0 END
                    OR a.rejected <> CASE WHEN o.state='rejected' THEN 1 ELSE 0 END
                    OR a.debited <> CASE WHEN o.state IN ('debited','delivered','refunded') THEN 1 ELSE 0 END
                    OR a.delivered <> CASE WHEN o.state='delivered' THEN 1 ELSE 0 END
                    OR a.refunded <> CASE WHEN o.state='refunded' THEN 1 ELSE 0 END
                    OR EXISTS (SELECT 1 FROM warehouse_events e WHERE e.operation_id=o.operation_id AND
                        (e.user_id<>o.user_id OR e.server_id<>o.server_id OR e.inventory_id<>o.inventory_id
                        OR e.item_id<>o.item_id OR e.item_data_hash<>o.item_data_hash
                        OR e.quantity<>o.quantity OR e.direction<>o.direction))`).Scan(&result.AuditStructureMismatches).Error; err != nil {
						t.Fatal(err)
					}
					result.AuditStructureChecked = true
					result.Consistent = quantity == expected && unsettled == result.UnconfirmedDebits && mismatches == 0 && result.AuditStructureMismatches == 0
					results = append(results, result)
					t.Logf("ops/s=%.1f p95=%.2fms p99=%.2fms errors=%v consistent=%v", result.OperationsPerSecond, result.P95MS, result.P99MS, result.Errors, result.Consistent)
					// 压力下的超时与服务错误保留为测量结果，不冒称请求全部成功
					unexpectedError := false
					for code := range result.WarmErrors {
						if code != "DeadlineExceeded" && code != "Canceled" && code != "Internal" && code != "Unavailable" && code != "ResourceExhausted" {
							unexpectedError = true
						}
					}
					for code := range result.Errors {
						if code != "DeadlineExceeded" && code != "Canceled" && code != "Internal" && code != "Unavailable" && code != "ResourceExhausted" {
							unexpectedError = true
						}
					}
					if unexpectedError || !result.Consistent {
						t.Errorf("压力结果异常：errors=%v quantity=%d expected=%d unsettled=%d mismatches=%d", result.Errors, quantity, expected, unsettled, mismatches)
					}
				})
			}
		}
	}
	t.Logf("schema=%s output=%s", schema, output)
}

// warehouseLoadPayload 构造含不可压缩 16 KiB 自定义数据的合法 NBT，仅用于中心载荷压力
// 不作为实际第三方模组 Codec 兼容性证据
func warehouseLoadPayload() []byte {
	payload := []byte{10, 0, 0, 8, 0, 2, 'i', 'd', 0, 15}
	payload = append(payload, []byte("minecraft:stone")...)
	payload = append(payload, 10, 0, 10)
	payload = append(payload, []byte("components")...)
	payload = append(payload, 10, 0, 21)
	payload = append(payload, []byte("minecraft:custom_data")...)
	payload = append(payload, 7, 0, 4, 'b', 'l', 'o', 'b')
	payload = binary.BigEndian.AppendUint32(payload, 16*1024)
	source := rand.New(rand.NewPCG(1, 2))
	for i := 0; i < 16*1024; i++ {
		payload = append(payload, byte(source.Uint32()))
	}
	return append(payload, 0, 0, 0)
}
