// 本文件验证名单条件的类型、路径、数字精度及黑白名单判定顺序

package itemdata

import (
	"encoding/binary"
	"errors"
	"testing"
)

// TestRuleMatching 验证完整键名、列表索引、严格数值类型及大整数不会被隐式转换
func TestRuleMatching(t *testing.T) {
	fields := testCompound(
		testField(tagLong, "credit", binary.BigEndian.AppendUint64(nil, 9007199254740993)),
		testField(tagByte, "flag", []byte{1}),
		testField(tagFloat, "zero", []byte{0x80, 0, 0, 0}),
		testField(tagList, "values", []byte{tagInt, 0, 0, 0, 2, 0, 0, 0, 4, 0, 0, 0, 5}),
		testField(tagByteArray, "array", []byte{0, 0, 0, 1, 1}),
	)
	document := testDocument(t, testRoot(
		testField(tagString, "id", testString("minecraft:stone")),
		testField(tagCompound, "minecraft:custom.data", fields),
	))
	index := 1
	firstIndex := 0
	for _, test := range []struct {
		name     string
		path     []PathPart
		typeName string
		value    string
		want     bool
	}{
		{"precise long", []PathPart{testKey("credit")}, "long", "9007199254740993", true},
		{"rounded long", []PathPart{testKey("credit")}, "long", "9007199254740992", false},
		{"byte", []PathPart{testKey("flag")}, "byte", "1", true},
		{"different type", []PathPart{testKey("flag")}, "int", "1", false},
		{"float zero", []PathPart{testKey("zero")}, "float", "0", true},
		{"list index", []PathPart{testKey("values"), {Index: &index}}, "int", "5", true},
		{"array index", []PathPart{testKey("array"), {Index: &firstIndex}}, "byte", "1", false},
		{"missing", []PathPart{testKey("missing")}, "int", "1", false},
		{"scalar index", []PathPart{testKey("flag"), {Index: &index}}, "byte", "1", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := append([]PathPart{testKey("minecraft:custom.data")}, test.path...)
			rule := Rule{Conditions: []Condition{{Path: path, Operator: OperatorTypedEquals, Type: test.typeName, Value: []byte(test.value)}}}
			got, err := document.Matches(rule)
			if err != nil || got != test.want {
				t.Fatalf("got %v, %v, want %v", got, err, test.want)
			}
		})
	}
	exists := Condition{Path: []PathPart{testKey("minecraft:custom.data"), testKey("flag")}, Operator: OperatorExists}
	for _, rule := range []Rule{{ItemID: "minecraft:stone"}, {Conditions: []Condition{exists}}, {ItemID: "minecraft:stone", Conditions: []Condition{exists}}} {
		if matched, err := document.Matches(rule); err != nil || !matched {
			t.Fatal("supported rule form did not match", err)
		}
	}
	if matched, err := document.Matches(Rule{ItemID: "minecraft:dirt", Conditions: []Condition{exists}}); err != nil || matched {
		t.Fatal("combined rule ignored item identifier", err)
	}
	missing := Condition{Path: []PathPart{testKey("missing")}, Operator: OperatorExists}
	if matched, err := document.Matches(Rule{Conditions: []Condition{exists, missing}}); err != nil || matched {
		t.Fatal("conditions must all match", err)
	}
}

// TestRuleValidation 拒绝无意义规则、非法路径、越界数值及错误操作符组合
func TestRuleValidation(t *testing.T) {
	key := "id"
	index := -1
	for _, condition := range []Condition{
		{Operator: OperatorExists},
		{Path: []PathPart{{}}, Operator: OperatorExists},
		{Path: []PathPart{{Key: &key, Index: &index}}, Operator: OperatorExists},
		{Path: []PathPart{{Index: &index}}, Operator: OperatorExists},
		{Path: []PathPart{testKey("id")}, Operator: OperatorExists, Type: "string"},
		{Path: []PathPart{testKey("id")}, Operator: OperatorExists, Value: []byte("null")},
		{Path: []PathPart{testKey("id")}, Operator: "contains"},
		{Path: []PathPart{testKey("id")}, Operator: OperatorTypedEquals, Type: "byte", Value: []byte("128")},
		{Path: []PathPart{testKey("id")}, Operator: OperatorTypedEquals, Type: "int", Value: []byte("1.0")},
		{Path: []PathPart{testKey("id")}, Operator: OperatorTypedEquals, Type: "long", Value: []byte(`"1"`)},
		{Path: []PathPart{testKey("id")}, Operator: OperatorTypedEquals, Type: "long", Value: []byte("9223372036854775808")},
		{Path: []PathPart{testKey("id")}, Operator: OperatorTypedEquals, Type: "float", Value: []byte("1e300")},
		{Path: []PathPart{testKey("id")}, Operator: OperatorTypedEquals, Type: "double", Value: []byte("NaN")},
		{Path: []PathPart{testKey("id")}, Operator: OperatorTypedEquals, Type: "string", Value: []byte("null")},
		{Path: []PathPart{testKey("id")}, Operator: OperatorTypedEquals, Type: "list", Value: []byte("[]")},
	} {
		if err := (Rule{Conditions: []Condition{condition}}).Validate(); !errors.Is(err, ErrRule) {
			t.Fatalf("accepted invalid condition %#v: %v", condition, err)
		}
	}
	for _, rule := range []Rule{{}, {ItemID: "bad"}, {ItemID: "minecraft:stone", Conditions: make([]Condition, MaxRuleConditions+1)}} {
		if err := rule.Validate(); !errors.Is(err, ErrRule) {
			t.Fatal("accepted invalid rule", err)
		}
	}
	path := make([]PathPart, MaxRulePath+1)
	for index := range path {
		path[index] = testKey("x")
	}
	if err := (Rule{Conditions: []Condition{{Path: path, Operator: OperatorExists}}}).Validate(); !errors.Is(err, ErrRule) {
		t.Fatal("accepted excessive path depth", err)
	}
}

// TestPolicyOrder 验证黑名单优先、默认允许以及白名单模式没有命中时拒绝
func TestPolicyOrder(t *testing.T) {
	document := testDocument(t, testRoot(testField(tagString, "id", testString("minecraft:stone"))))
	stone := Rule{ItemID: "minecraft:stone"}
	dirt := Rule{ItemID: "minecraft:dirt"}
	for _, test := range []struct {
		policy Policy
		want   bool
	}{
		{Policy{}, true},
		{Policy{Blacklist: []Rule{dirt}}, true},
		{Policy{Blacklist: []Rule{stone}}, false},
		{Policy{OnlyWhitelist: true}, false},
		{Policy{OnlyWhitelist: true, Whitelist: []Rule{dirt}}, false},
		{Policy{OnlyWhitelist: true, Whitelist: []Rule{dirt, stone}}, true},
		{Policy{OnlyWhitelist: true, Whitelist: []Rule{stone}, Blacklist: []Rule{stone}}, false},
	} {
		if got, err := document.Allowed(test.policy); err != nil || got != test.want {
			t.Fatalf("policy %#v: got %v, %v; want %v", test.policy, got, err, test.want)
		}
	}
	if _, err := document.Allowed(Policy{Whitelist: []Rule{{}}}); !errors.Is(err, ErrRule) {
		t.Fatal("invalid unused whitelist was silently accepted", err)
	}
	if err := (Policy{Blacklist: make([]Rule, MaxPolicyRules+1)}).Validate(); !errors.Is(err, ErrRule) {
		t.Fatal("accepted too many rules", err)
	}
}
