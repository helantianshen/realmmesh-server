// 共享仓库数据库验证：在显式指定的测试连接中创建独立 schema 验证并发和回滚

package repo_test

import (
	"bytes"
	"context"
	"errors"
	"math"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/helantianshen/realmmesh-server/internal/data"
	"github.com/helantianshen/realmmesh-server/internal/model"
	"github.com/helantianshen/realmmesh-server/internal/repo"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type warehouseFixture struct {
	db   *gorm.DB
	repo *repo.WarehouseRepository
	ids  atomic.Int64
}

// TestWarehousePostgres 验证真实 PostgreSQL 的唯一索引、条件更新和事务原子性
func TestWarehousePostgres(t *testing.T) {
	fixture := openWarehouseFixture(t)
	t.Run("parallel_first_deposit", fixture.parallelFirstDeposit)
	t.Run("parallel_repeated_deposit", fixture.parallelRepeatedDeposit)
	t.Run("competing_withdrawals", fixture.competingWithdrawals)
	t.Run("parallel_repeated_withdrawal", fixture.parallelRepeatedWithdrawal)
	t.Run("finish_race", fixture.finishRace)
	t.Run("mutation_result_and_audit", fixture.mutationResultAndAudit)
	t.Run("empty_redeposit_refund", fixture.emptyRedepositRefund)
	t.Run("audit_failure_rolls_back", fixture.auditFailureRollsBack)
	t.Run("hash_collision_and_unstackable", fixture.hashCollisionAndUnstackable)
	t.Run("request_binding_and_soft_delete", fixture.requestBindingAndSoftDelete)
	t.Run("quantity_limit_and_overflow", fixture.quantityLimitAndOverflow)
	t.Run("deposit_policy_idempotency", fixture.depositPolicyIdempotency)
	t.Run("retry_after_concurrent_empty", fixture.retryAfterConcurrentEmpty)
}

// openWarehouseFixture 只读取显式测试 DSN，清理范围限于本次随机生成的 schema
func openWarehouseFixture(t *testing.T) *warehouseFixture {
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
	schema := "warehouse_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := adminPool.ExecContext(t.Context(), "CREATE SCHEMA "+schema); err != nil {
		t.Fatal("无法创建独立仓库测试 schema")
	}
	t.Cleanup(func() {
		if _, err := adminPool.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error("清理独立仓库测试 schema 失败")
		}
	})

	configuration.RuntimeParams["search_path"] = schema
	configuration.RuntimeParams["statement_timeout"] = "10000"
	pool := stdlib.OpenDB(*configuration)
	pool.SetMaxOpenConns(12)
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{
		SkipDefaultTransaction: true,
		Logger:                 logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal("无法初始化仓库测试连接")
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.AutoMigrate(&model.WarehouseItem{}, &model.WarehouseOperation{}, &model.WarehouseEvent{}); err != nil {
			return err
		}
		return data.MigrateWarehouseConstraints(tx)
	})
	if err != nil {
		t.Fatalf("迁移独立仓库测试 schema: %v", err)
	}
	fixture := &warehouseFixture{db: db, repo: repo.NewWarehouseRepository(db)}
	fixture.ids.Store(1000)
	return fixture
}

// mutation 生成互不重复的测试主键，载荷在仓储测试中作为不透明规范化字节处理
func (fixture *warehouseFixture) mutation(userID, quantity int64) repo.WarehouseMutation {
	return repo.WarehouseMutation{
		RecordID: fixture.ids.Add(1), EventID: fixture.ids.Add(1), InventoryID: fixture.ids.Add(1),
		UserID: userID, ServerID: 1, OperationID: uuid.NewString(), ItemID: "minecraft:stone",
		ItemData: []byte{10, 0, 0, 1, 2, 3}, Stackable: true, Quantity: quantity, QuantityLimit: math.MaxInt32,
	}
}

// deposit 写入测试初始库存，失败时立即结束当前验证
func (fixture *warehouseFixture) deposit(t *testing.T, input repo.WarehouseMutation) *model.WarehouseOperation {
	t.Helper()
	operation, err := fixture.repo.Deposit(t.Context(), input)
	if err != nil {
		t.Fatalf("存入失败: %v", err)
	}
	return operation
}

// debit 绑定已存在的库存身份，创建独立取出操作
func (fixture *warehouseFixture) debit(t *testing.T, deposit *model.WarehouseOperation, quantity int64) *model.WarehouseOperation {
	t.Helper()
	input := fixture.mutation(deposit.UserID, quantity)
	input.InventoryID, input.ItemID, input.ItemData, input.Stackable = deposit.InventoryID, deposit.ItemID, deposit.ItemData, deposit.Stackable
	operation, err := fixture.repo.Debit(t.Context(), input)
	if err != nil {
		t.Fatalf("扣库失败: %v", err)
	}
	return operation
}

// finishInput 为同一取出操作建立独立确认请求
func (fixture *warehouseFixture) finishInput(operation *model.WarehouseOperation, delivered bool) repo.WarehouseFinish {
	return repo.WarehouseFinish{
		OperationID: operation.OperationID, UserID: operation.UserID, ServerID: operation.ServerID,
		EventID: fixture.ids.Add(1), Delivered: delivered, Reason: "test_result",
	}
}

// quantity 直接读取含零数量记录，核对条件更新后的持久化结果
func (fixture *warehouseFixture) quantity(t *testing.T, inventoryID int64) int64 {
	t.Helper()
	var item model.WarehouseItem
	if err := fixture.db.First(&item, inventoryID).Error; err != nil {
		t.Fatalf("读取库存失败: %v", err)
	}
	return item.Quantity
}

// parallelFirstDeposit 验证相同物品首次并发插入只生成一条库存且不丢数量
func (fixture *warehouseFixture) parallelFirstDeposit(t *testing.T) {
	const requests = 24
	inputs := make([]repo.WarehouseMutation, requests)
	for index := range inputs {
		inputs[index] = fixture.mutation(101, 3)
	}
	warehouseParallel(t, requests, func(index int) error {
		_, err := fixture.repo.Deposit(t.Context(), inputs[index])
		return err
	})
	items, err := fixture.repo.ListItems(t.Context(), 101, 0, 100)
	if err != nil || len(items) != 1 || items[0].Quantity != requests*3 {
		t.Fatalf("并发首次入库结果不符: items=%+v err=%v", items, err)
	}
	if len(items[0].ItemData) != 0 {
		t.Fatal("列表读取了完整物品载荷")
	}
	item, err := fixture.repo.FindItem(t.Context(), 101, items[0].ID)
	if err != nil || len(item.ItemData) == 0 {
		t.Fatalf("按 ID 读取未保留完整载荷: item=%+v err=%v", item, err)
	}
}

// parallelRepeatedDeposit 验证重复请求及不同候选主键不能重复增加库存
func (fixture *warehouseFixture) parallelRepeatedDeposit(t *testing.T) {
	const requests = 16
	initial := fixture.mutation(102, 7)
	warehouseParallel(t, requests, func(index int) error {
		input := initial
		input.RecordID, input.InventoryID, input.EventID = fixture.ids.Add(1), fixture.ids.Add(1), fixture.ids.Add(1)
		_, err := fixture.repo.Deposit(t.Context(), input)
		return err
	})
	items, err := fixture.repo.ListItems(t.Context(), 102, 0, 100)
	if err != nil || len(items) != 1 || items[0].Quantity != 7 {
		t.Fatalf("重复入库改变库存: items=%+v err=%v", items, err)
	}
	var count int64
	fixture.db.Model(&model.WarehouseEvent{}).Where("operation_id = ?", initial.OperationID).Count(&count)
	if count != 1 {
		t.Fatalf("重复入库审计条数=%d，期望 1", count)
	}
}

// competingWithdrawals 验证两次预查通过后只有库存足够的一次实际扣减成功
func (fixture *warehouseFixture) competingWithdrawals(t *testing.T) {
	deposit := fixture.deposit(t, fixture.mutation(103, 100))
	var succeeded atomic.Int64
	var failed atomic.Int64
	warehouseParallel(t, 2, func(index int) error {
		input := fixture.mutation(103, 80)
		input.InventoryID = deposit.InventoryID
		_, err := fixture.repo.Debit(t.Context(), input)
		if errors.Is(err, repo.ErrWarehouseItemNotFound) {
			failed.Add(1)
			return nil
		}
		if err == nil {
			succeeded.Add(1)
		}
		return err
	})
	if succeeded.Load() != 1 || failed.Load() != 1 || fixture.quantity(t, deposit.InventoryID) != 20 {
		t.Fatalf("竞争扣库结果不符: success=%d failed=%d", succeeded.Load(), failed.Load())
	}
}

// parallelRepeatedWithdrawal 验证重复准备确认只扣库一次，完成后的重试返回原终态
func (fixture *warehouseFixture) parallelRepeatedWithdrawal(t *testing.T) {
	deposit := fixture.deposit(t, fixture.mutation(111, 100))
	initial := fixture.mutation(111, 20)
	initial.InventoryID = deposit.InventoryID
	warehouseParallel(t, 12, func(index int) error {
		input := initial
		input.RecordID, input.EventID = fixture.ids.Add(1), fixture.ids.Add(1)
		_, err := fixture.repo.Debit(t.Context(), input)
		return err
	})
	if fixture.quantity(t, deposit.InventoryID) != 80 {
		t.Fatal("重复准备确认多次扣库")
	}
	operation, err := fixture.repo.FindOperation(t.Context(), 111, 1, initial.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repo.Finish(t.Context(), fixture.finishInput(operation, true)); err != nil {
		t.Fatal(err)
	}
	initial.RecordID, initial.EventID = fixture.ids.Add(1), fixture.ids.Add(1)
	repeated, err := fixture.repo.Debit(t.Context(), initial)
	if err != nil || repeated.State != model.WarehouseDelivered || fixture.quantity(t, deposit.InventoryID) != 80 {
		t.Fatalf("完成后重试未返回原终态: operation=%+v err=%v", repeated, err)
	}
}

// mutationResultAndAudit 验证窄 RETURNING 保留完整响应、实际库存绑定和终态时间
func (fixture *warehouseFixture) mutationResultAndAudit(t *testing.T) {
	for index, delivered := range []bool{true, false} {
		userID := int64(120 + index)
		seed := fixture.deposit(t, fixture.mutation(userID, 20))
		deposit := fixture.deposit(t, fixture.mutation(userID, 10))
		if deposit.InventoryID != seed.InventoryID {
			t.Fatal("合并存入未返回实际库存 ID")
		}
		stored, err := fixture.repo.FindOperation(t.Context(), userID, 1, deposit.OperationID)
		if err != nil || stored.InventoryID != deposit.InventoryID || !stored.UpdatedAt.Equal(deposit.UpdatedAt) {
			t.Fatalf("存入返回值与持久化操作不一致: %v", err)
		}
		var event model.WarehouseEvent
		if err := fixture.db.Where("operation_id = ?", deposit.OperationID).Take(&event).Error; err != nil {
			t.Fatal(err)
		}
		if event.InventoryID != seed.InventoryID || event.UserID != userID || event.ServerID != deposit.ServerID ||
			event.ItemID != deposit.ItemID || !bytes.Equal(event.ItemDataHash, deposit.ItemDataHash) ||
			event.Quantity != deposit.Quantity || event.Direction != deposit.Direction || event.State != deposit.State || event.Reason != deposit.Reason {
			t.Fatal("存入审计未完整引用实际操作")
		}
		withdrawal := fixture.debit(t, deposit, 5)
		input := fixture.finishInput(withdrawal, delivered)
		input.Reason = "confirmed"
		finished, err := fixture.repo.Finish(t.Context(), input)
		if err != nil {
			t.Fatal(err)
		}
		stored, err = fixture.repo.FindOperation(t.Context(), userID, 1, withdrawal.OperationID)
		if err != nil {
			t.Fatal(err)
		}
		if finished.State != stored.State || finished.Reason != "confirmed" || finished.Reason != stored.Reason ||
			!finished.UpdatedAt.Equal(stored.UpdatedAt) || !finished.UpdatedAt.After(withdrawal.UpdatedAt) ||
			finished.InventoryID != stored.InventoryID || finished.Quantity != stored.Quantity ||
			!bytes.Equal(finished.ItemData, stored.ItemData) || !bytes.Equal(finished.ItemDataHash, stored.ItemDataHash) {
			t.Fatal("完成返回值与持久化终态不一致")
		}
		input.EventID = fixture.ids.Add(1)
		input.Reason = "retry must preserve first reason"
		repeated, err := fixture.repo.Finish(t.Context(), input)
		if err != nil || repeated.Reason != finished.Reason || !repeated.UpdatedAt.Equal(finished.UpdatedAt) {
			t.Fatalf("重复完成未保留原终态: %v", err)
		}
	}
}

// finishRace 验证发放确认与退款竞争只允许一个终态，且重复终态不重复变更
func (fixture *warehouseFixture) finishRace(t *testing.T) {
	deposit := fixture.deposit(t, fixture.mutation(104, 100))
	withdrawal := fixture.debit(t, deposit, 80)
	warehouseParallel(t, 20, func(index int) error {
		_, err := fixture.repo.Finish(t.Context(), fixture.finishInput(withdrawal, index%2 == 0))
		if errors.Is(err, repo.ErrWarehouseOperationConflict) {
			return nil
		}
		return err
	})
	operation, err := fixture.repo.FindOperation(t.Context(), 104, 1, withdrawal.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	expected := int64(20)
	if operation.State == model.WarehouseRefunded {
		expected = 100
	} else if operation.State != model.WarehouseDelivered {
		t.Fatalf("错误终态: %s", operation.State)
	}
	if actual := fixture.quantity(t, deposit.InventoryID); actual != expected {
		t.Fatalf("终态 %s 库存=%d，期望 %d", operation.State, actual, expected)
	}
	var count int64
	fixture.db.Model(&model.WarehouseEvent{}).Where("operation_id = ?", withdrawal.OperationID).Count(&count)
	if count != 2 {
		t.Fatalf("扣库及完成审计条数=%d，期望 2", count)
	}
}

// emptyRedepositRefund 验证扣空后再次存入不会阻止原操作退库，退款允许越过存入业务上限
func (fixture *warehouseFixture) emptyRedepositRefund(t *testing.T) {
	input := fixture.mutation(105, 100)
	input.QuantityLimit = 100
	deposit := fixture.deposit(t, input)
	withdrawal := fixture.debit(t, deposit, 100)
	item, err := fixture.repo.FindItem(t.Context(), 105, deposit.InventoryID)
	if err != nil || item.Quantity != 0 {
		t.Fatalf("零库存身份未保留: item=%+v err=%v", item, err)
	}
	items, err := fixture.repo.ListItems(t.Context(), 105, 0, 100)
	if err != nil || len(items) != 0 {
		t.Fatalf("列表包含零库存: items=%+v err=%v", items, err)
	}
	input = fixture.mutation(105, 100)
	input.QuantityLimit = 100
	if operation := fixture.deposit(t, input); operation.InventoryID != deposit.InventoryID {
		t.Fatal("零库存被另建记录替代")
	}
	warehouseParallel(t, 12, func(index int) error {
		_, err := fixture.repo.Finish(t.Context(), fixture.finishInput(withdrawal, false))
		return err
	})
	if actual := fixture.quantity(t, deposit.InventoryID); actual != 200 {
		t.Fatalf("扣空重入后退款数量=%d，期望 200", actual)
	}
	input = fixture.mutation(105, 1)
	input.QuantityLimit = 100
	if _, err := fixture.repo.Deposit(t.Context(), input); !errors.Is(err, repo.ErrWarehouseQuantityLimit) {
		t.Fatalf("已超限库存仍可新存入: %v", err)
	}
}

// auditFailureRollsBack 验证审计插入失败不能留下库存变化或已完成状态
func (fixture *warehouseFixture) auditFailureRollsBack(t *testing.T) {
	seed := fixture.mutation(106, 50)
	deposit := fixture.deposit(t, seed)
	input := fixture.mutation(106, 10)
	input.EventID = seed.EventID
	if _, err := fixture.repo.Deposit(t.Context(), input); !errors.Is(err, repo.ErrRepository) {
		t.Fatalf("重复审计主键未触发事务失败: %v", err)
	}
	if fixture.quantity(t, deposit.InventoryID) != 50 {
		t.Fatal("审计失败后库存增加未回滚")
	}
	if _, err := fixture.repo.FindOperation(t.Context(), 106, 1, input.OperationID); !errors.Is(err, repo.ErrWarehouseOperationNotFound) {
		t.Fatalf("审计失败后仍保存操作: %v", err)
	}
	withdrawal := fixture.debit(t, deposit, 50)
	before, err := fixture.repo.FindOperation(t.Context(), 106, 1, withdrawal.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	delivered := fixture.finishInput(withdrawal, true)
	delivered.EventID = seed.EventID
	if _, err := fixture.repo.Finish(t.Context(), delivered); !errors.Is(err, repo.ErrRepository) {
		t.Fatalf("发放确认审计失败未返回仓储错误: %v", err)
	}
	unchanged, err := fixture.repo.FindOperation(t.Context(), 106, 1, withdrawal.OperationID)
	if err != nil || unchanged.State != model.WarehouseDebited || !unchanged.UpdatedAt.Equal(before.UpdatedAt) || fixture.quantity(t, deposit.InventoryID) != 0 {
		t.Fatalf("发放确认审计失败未回滚终态: operation=%+v err=%v", unchanged, err)
	}
	finish := fixture.finishInput(withdrawal, false)
	finish.EventID = seed.EventID
	if _, err := fixture.repo.Finish(t.Context(), finish); !errors.Is(err, repo.ErrRepository) {
		t.Fatalf("退款审计失败未返回仓储错误: %v", err)
	}
	operation, err := fixture.repo.FindOperation(t.Context(), 106, 1, withdrawal.OperationID)
	if err != nil || operation.State != model.WarehouseDebited || fixture.quantity(t, deposit.InventoryID) != 0 {
		t.Fatalf("退款审计失败未回滚状态和数量: operation=%+v err=%v", operation, err)
	}
	if _, err := fixture.repo.Finish(t.Context(), fixture.finishInput(withdrawal, false)); err != nil {
		t.Fatal(err)
	}
}

// hashCollisionAndUnstackable 验证完整载荷复核以及不可堆叠物品逐件保存
func (fixture *warehouseFixture) hashCollisionAndUnstackable(t *testing.T) {
	deposit := fixture.deposit(t, fixture.mutation(107, 10))
	// 保持原指纹但置换载荷，模拟相同哈希对应不同完整数据的存量候选
	if err := fixture.db.Model(&model.WarehouseItem{}).Where("id = ?", deposit.InventoryID).
		Update("item_data", []byte("different-data-with-same-hash")).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repo.Deposit(t.Context(), fixture.mutation(107, 1)); !errors.Is(err, repo.ErrWarehouseHashCollision) {
		t.Fatalf("哈希碰撞未拒绝: %v", err)
	}
	if fixture.quantity(t, deposit.InventoryID) != 10 {
		t.Fatal("哈希碰撞改变了库存数量")
	}
	for range 2 {
		input := fixture.mutation(108, 1)
		input.Stackable = false
		fixture.deposit(t, input)
	}
	items, err := fixture.repo.ListItems(t.Context(), 108, 0, 100)
	if err != nil || len(items) != 2 || items[0].Quantity != 1 || items[1].Quantity != 1 {
		t.Fatalf("不可堆叠物品被合并: items=%+v err=%v", items, err)
	}
}

// requestBindingAndSoftDelete 验证同一操作不能更换参数，软删除也不能解除幂等
func (fixture *warehouseFixture) requestBindingAndSoftDelete(t *testing.T) {
	input := fixture.mutation(109, 10)
	deposit := fixture.deposit(t, input)
	changed := input
	changed.Quantity++
	if _, err := fixture.repo.Deposit(t.Context(), changed); !errors.Is(err, repo.ErrWarehouseOperationConflict) {
		t.Fatalf("同一操作更换数量未拒绝: %v", err)
	}
	if _, err := fixture.repo.FindOperation(t.Context(), 109, 2, input.OperationID); !errors.Is(err, repo.ErrWarehouseOperationNotFound) {
		t.Fatalf("其他服务器可读取原操作: %v", err)
	}
	if err := fixture.db.Delete(&model.WarehouseOperation{}, deposit.ID).Error; err != nil {
		t.Fatal(err)
	}
	input.RecordID, input.InventoryID, input.EventID = fixture.ids.Add(1), fixture.ids.Add(1), fixture.ids.Add(1)
	fixture.deposit(t, input)
	if fixture.quantity(t, deposit.InventoryID) != 10 {
		t.Fatal("操作软删除后发生重复入库")
	}
	input.QuantityLimit = 1
	fixture.deposit(t, input)
	changed = fixture.mutation(109, 1)
	changed.InventoryID = deposit.InventoryID
	changed.ItemData = []byte("different-prepared-item")
	if _, err := fixture.repo.Debit(t.Context(), changed); !errors.Is(err, repo.ErrWarehouseItemNotFound) {
		t.Fatalf("取出更换准备载荷未拒绝: %v", err)
	}
}

// quantityLimitAndOverflow 验证首次写入上限和退款整数溢出不会绕过数量保护
func (fixture *warehouseFixture) quantityLimitAndOverflow(t *testing.T) {
	input := fixture.mutation(110, 2)
	input.QuantityLimit = 1
	if _, err := fixture.repo.Deposit(t.Context(), input); !errors.Is(err, repo.ErrWarehouseQuantityLimit) {
		t.Fatalf("首次插入未检查上限: %v", err)
	}
	input.RecordID, input.InventoryID, input.EventID = fixture.ids.Add(1), fixture.ids.Add(1), fixture.ids.Add(1)
	input.QuantityLimit = 2
	if _, err := fixture.repo.Deposit(t.Context(), input); !errors.Is(err, repo.ErrWarehouseQuantityLimit) {
		t.Fatalf("上限拒绝后同一操作重新入库: %v", err)
	}
	input = fixture.mutation(110, math.MaxInt64)
	input.QuantityLimit = math.MaxInt64
	deposit := fixture.deposit(t, input)
	withdrawal := fixture.debit(t, deposit, 1)
	input = fixture.mutation(110, 1)
	input.QuantityLimit = math.MaxInt64
	fixture.deposit(t, input)
	if _, err := fixture.repo.Finish(t.Context(), fixture.finishInput(withdrawal, false)); !errors.Is(err, repo.ErrWarehouseStateConflict) {
		t.Fatalf("退款整数溢出未拒绝: %v", err)
	}
	operation, err := fixture.repo.FindOperation(t.Context(), 110, 1, withdrawal.OperationID)
	if err != nil || operation.State != model.WarehouseDebited || fixture.quantity(t, deposit.InventoryID) != math.MaxInt64 {
		t.Fatalf("溢出退款未保留未完成操作: operation=%+v err=%v", operation, err)
	}
}

// depositPolicyIdempotency 验证名单变化和并发重试不会让同一操作同时得到成功与明确拒绝
func (fixture *warehouseFixture) depositPolicyIdempotency(t *testing.T) {
	allowed := fixture.mutation(112, 10)
	deposit := fixture.deposit(t, allowed)
	allowed.RecordID, allowed.InventoryID, allowed.EventID = fixture.ids.Add(1), fixture.ids.Add(1), fixture.ids.Add(1)
	allowed.DepositDenied = true
	if repeated := fixture.deposit(t, allowed); repeated.InventoryID != deposit.InventoryID {
		t.Fatal("新名单改变了已提交操作")
	}
	denied := fixture.mutation(113, 10)
	denied.DepositDenied = true
	operation, err := fixture.repo.Deposit(t.Context(), denied)
	if !errors.Is(err, repo.ErrWarehouseDepositDenied) || operation == nil || operation.State != model.WarehouseRejected {
		t.Fatalf("名单拒绝未保存稳定终态: operation=%+v err=%v", operation, err)
	}
	denied.RecordID, denied.InventoryID, denied.EventID = fixture.ids.Add(1), fixture.ids.Add(1), fixture.ids.Add(1)
	denied.DepositDenied = false
	if _, err := fixture.repo.Deposit(t.Context(), denied); !errors.Is(err, repo.ErrWarehouseDepositDenied) {
		t.Fatalf("已拒绝操作因名单变化再次入库: %v", err)
	}
	items, err := fixture.repo.ListItems(t.Context(), 113, 0, 100)
	if err != nil || len(items) != 0 {
		t.Fatalf("拒绝操作仍产生库存: items=%+v err=%v", items, err)
	}

	initial := fixture.mutation(114, 10)
	var succeeded atomic.Int64
	var rejected atomic.Int64
	warehouseParallel(t, 20, func(index int) error {
		input := initial
		input.RecordID, input.InventoryID, input.EventID = fixture.ids.Add(1), fixture.ids.Add(1), fixture.ids.Add(1)
		input.DepositDenied = index%2 == 0
		_, err := fixture.repo.Deposit(t.Context(), input)
		if errors.Is(err, repo.ErrWarehouseDepositDenied) {
			rejected.Add(1)
			return nil
		}
		if err == nil {
			succeeded.Add(1)
		}
		return err
	})
	if succeeded.Load() != 20 && rejected.Load() != 20 {
		t.Fatalf("相同操作产生不同明确结论: success=%d rejected=%d", succeeded.Load(), rejected.Load())
	}
	var events int64
	if err := fixture.db.Model(&model.WarehouseEvent{}).Where("operation_id = ?", initial.OperationID).Count(&events).Error; err != nil {
		t.Fatal(err)
	}
	if events != 1 {
		t.Fatalf("名单竞态重复记录首次结论: events=%d", events)
	}
}

// retryAfterConcurrentEmpty 复现重复请求先未查到操作、再遇到另一请求已扣空的交错
func (fixture *warehouseFixture) retryAfterConcurrentEmpty(t *testing.T) {
	deposit := fixture.deposit(t, fixture.mutation(115, 10))
	input := fixture.mutation(115, 10)
	input.InventoryID = deposit.InventoryID
	if _, err := fixture.repo.FindOperation(t.Context(), 115, 1, input.OperationID); !errors.Is(err, repo.ErrWarehouseOperationNotFound) {
		t.Fatalf("首次读取操作结果不符: %v", err)
	}

	// 让另一请求在本请求读取原物品前完成扣空，固定触发曾经的竞态窗口
	done := make(chan error, 1)
	go func() {
		_, err := fixture.repo.Debit(t.Context(), input)
		done <- err
	}()
	if err := <-done; err != nil {
		t.Fatalf("并发扣空失败: %v", err)
	}
	item, err := fixture.repo.FindItem(t.Context(), 115, input.InventoryID)
	if err != nil || item.Quantity != 0 || len(item.ItemData) == 0 {
		t.Fatalf("重复请求无法复核零库存身份: item=%+v err=%v", item, err)
	}
	input.RecordID, input.EventID = fixture.ids.Add(1), fixture.ids.Add(1)
	input.ItemID, input.ItemData, input.Stackable = item.ItemID, item.ItemData, item.Stackable
	operation, err := fixture.repo.Debit(t.Context(), input)
	if err != nil || operation.State != model.WarehouseDebited || fixture.quantity(t, deposit.InventoryID) != 0 {
		t.Fatalf("扣空后的重复请求未返回原结果: operation=%+v err=%v", operation, err)
	}
	var events int64
	if err := fixture.db.Model(&model.WarehouseEvent{}).Where("operation_id = ?", input.OperationID).Count(&events).Error; err != nil {
		t.Fatal(err)
	}
	if events != 1 {
		t.Fatalf("扣空后的重复请求写入额外审计: events=%d", events)
	}
}

// warehouseParallel 同时释放多个请求并收集全部错误，避免忽略后台验证失败
func warehouseParallel(t *testing.T, count int, apply func(int) error) {
	t.Helper()
	start := make(chan struct{})
	errors := make(chan error, count)
	var group sync.WaitGroup
	for index := range count {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			errors <- apply(index)
		}()
	}
	close(start)
	group.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Errorf("并发请求失败: %v", err)
		}
	}
}
