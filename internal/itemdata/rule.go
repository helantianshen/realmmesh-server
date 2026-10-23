// 本文件实现仓库名单的精确物品 ID、结构化路径和有类型标量匹配

package itemdata

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"unicode/utf16"
	"unicode/utf8"
)

const (
	OperatorExists      = "exists"
	OperatorTypedEquals = "typed_equals"
	MaxPolicyRules      = 256 // 黑白名单合计最多 256 条规则，超过时拒绝配置
	MaxRuleConditions   = 32  // 每条规则最多 32 个字段条件，超过时拒绝配置
	MaxRulePath         = 32  // 每个字段路径最多 32 段，超过时拒绝配置
)

// Policy 是一次读取和替换的完整名单策略，黑名单始终优先
type Policy struct {
	OnlyWhitelist bool   `json:"only_whitelist"`
	Blacklist     []Rule `json:"blacklist"`
	Whitelist     []Rule `json:"whitelist"`
}

// Rule 的物品 ID 与全部字段条件同时匹配，任意一项可以省略，但不能全部为空
type Rule struct {
	ItemID     string      `json:"item_id,omitempty"`
	Conditions []Condition `json:"conditions,omitempty"`
}

// Condition 的标量类型使用 NBT 类型名称，Value 保留 JSON 数字精度
type Condition struct {
	Path     []PathPart      `json:"path"`
	Operator string          `json:"operator"`
	Type     string          `json:"type,omitempty"`
	Value    json.RawMessage `json:"value,omitempty"`
}

// PathPart 每段只能选择一个完整 compound 键或非负列表索引，数组不支持索引
type PathPart struct {
	Key   *string `json:"key,omitempty"`
	Index *int    `json:"index,omitempty"`
}

// Validate 校验完整策略及规则数量，避免部分有效配置被意外生效
func (policy Policy) Validate() error {
	if len(policy.Blacklist) > MaxPolicyRules || len(policy.Whitelist) > MaxPolicyRules-len(policy.Blacklist) {
		return fmt.Errorf("%w: too many policy rules", ErrRule)
	}
	for _, rule := range policy.Blacklist {
		if err := rule.Validate(); err != nil {
			return err
		}
	}
	for _, rule := range policy.Whitelist {
		if err := rule.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// Allowed 按黑名单优先和可选白名单模式判断新存入，不改变既有库存的取出权限
func (document *Document) Allowed(policy Policy) (bool, error) {
	if err := policy.Validate(); err != nil {
		return false, err
	}
	if document == nil || document.root == nil {
		return false, ErrMalformed
	}
	for _, rule := range policy.Blacklist {
		matched, err := document.Matches(rule)
		if err != nil || matched {
			return false, err
		}
	}
	if !policy.OnlyWhitelist {
		return true, nil
	}
	for _, rule := range policy.Whitelist {
		matched, err := document.Matches(rule)
		if err != nil || matched {
			return matched, err
		}
	}
	return false, nil
}

// Validate 拒绝空规则、非法路径、操作符与标量值，保证配置失败时不会默认为允许
func (rule Rule) Validate() error {
	if rule.ItemID == "" && len(rule.Conditions) == 0 {
		return fmt.Errorf("%w: empty rule", ErrRule)
	}
	if rule.ItemID != "" && !validItemID(rule.ItemID) {
		return fmt.Errorf("%w: invalid item identifier", ErrRule)
	}
	if len(rule.Conditions) > MaxRuleConditions {
		return fmt.Errorf("%w: too many rule conditions", ErrRule)
	}
	for index, condition := range rule.Conditions {
		if _, err := condition.validate(); err != nil {
			return fmt.Errorf("condition %d: %w", index, err)
		}
	}
	return nil
}

// Matches 判断完整规则是否命中，字段缺失或路径类型不符返回 false
func (document *Document) Matches(rule Rule) (bool, error) {
	if err := rule.Validate(); err != nil {
		return false, err
	}
	if document == nil || document.root == nil {
		return false, ErrMalformed
	}
	if rule.ItemID != "" && rule.ItemID != document.ItemID {
		return false, nil
	}
	for _, condition := range rule.Conditions {
		actual := document.root.lookup(condition.Path)
		if actual == nil {
			return false, nil
		}
		if condition.Operator == OperatorExists {
			continue
		}
		expected, err := condition.scalar()
		if err != nil {
			return false, err
		}
		if !actual.scalarEqual(expected) {
			return false, nil
		}
	}
	return true, nil
}

// validate 校验每个路径段的单一含义，并检查操作符所需的值结构
func (condition Condition) validate() (*node, error) {
	if len(condition.Path) == 0 || len(condition.Path) > MaxRulePath {
		return nil, fmt.Errorf("%w: invalid path length", ErrRule)
	}
	for _, part := range condition.Path {
		if (part.Key == nil) == (part.Index == nil) {
			return nil, fmt.Errorf("%w: path part must contain exactly one key or index", ErrRule)
		}
		if part.Index != nil && *part.Index < 0 {
			return nil, fmt.Errorf("%w: negative list index", ErrRule)
		}
		if part.Key != nil && (len(*part.Key) > math.MaxUint16 || !utf8.ValidString(*part.Key) || len(modifiedUTF8(*part.Key)) > math.MaxUint16) {
			return nil, fmt.Errorf("%w: invalid compound key", ErrRule)
		}
	}
	switch condition.Operator {
	case OperatorExists:
		if condition.Type != "" || len(condition.Value) != 0 {
			return nil, fmt.Errorf("%w: exists does not accept a value", ErrRule)
		}
		return nil, nil
	case OperatorTypedEquals:
		return condition.scalar()
	default:
		return nil, fmt.Errorf("%w: unsupported operator", ErrRule)
	}
}

// lookup 逐段读取 compound 或列表，不将数组、数字键名或类型不符的节点隐式转换
func (value *node) lookup(path []PathPart) *node {
	for _, part := range path {
		if part.Key != nil {
			value = value.child(string(modifiedUTF8(*part.Key)))
		} else if value.tag == tagList && *part.Index < len(value.elements) {
			value = value.elements[*part.Index]
		} else {
			return nil
		}
		if value == nil {
			return nil
		}
	}
	return value
}

// scalar 保留整数精度并按指定 NBT 类型检查范围，不接受隐式字符串或数字转换
func (condition Condition) scalar() (*node, error) {
	data := bytes.TrimSpace(condition.Value)
	if !json.Valid(data) {
		return nil, fmt.Errorf("%w: invalid scalar JSON", ErrRule)
	}
	value := &node{}
	switch condition.Type {
	case "byte", "short", "int", "long":
		bits := 64
		switch condition.Type {
		case "byte":
			bits, value.tag = 8, tagByte
		case "short":
			bits, value.tag = 16, tagShort
		case "int":
			bits, value.tag = 32, tagInt
		case "long":
			value.tag = tagLong
		}
		// 十进制和明确位宽防止 JSON 大整数经过 float64 丢失精度
		number, err := strconv.ParseInt(string(data), 10, bits)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid integer value", ErrRule)
		}
		encoded := binary.BigEndian.AppendUint64(nil, uint64(number))
		value.raw = encoded[8-bits/8:]
	case "float", "double":
		bits := 64
		value.tag = tagDouble
		if condition.Type == "float" {
			bits, value.tag = 32, tagFloat
		}
		number, err := strconv.ParseFloat(string(data), bits)
		if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
			return nil, fmt.Errorf("%w: invalid floating point value", ErrRule)
		}
		if bits == 32 {
			value.raw = binary.BigEndian.AppendUint32(nil, math.Float32bits(float32(number)))
		} else {
			value.raw = binary.BigEndian.AppendUint64(nil, math.Float64bits(number))
		}
	case "string":
		var text string
		if len(data) == 0 || data[0] != '"' || json.Unmarshal(data, &text) != nil {
			return nil, fmt.Errorf("%w: invalid string value", ErrRule)
		}
		encoded := modifiedUTF8(text)
		if len(encoded) > math.MaxUint16 {
			return nil, fmt.Errorf("%w: string too long", ErrRule)
		}
		value.tag = tagString
		value.raw = binary.BigEndian.AppendUint16(nil, uint16(len(encoded)))
		value.raw = append(value.raw, encoded...)
	default:
		return nil, fmt.Errorf("%w: unsupported scalar type", ErrRule)
	}
	return value, nil
}

// scalarEqual 要求标签类型一致，浮点按数值相等处理，其余标量按完整编码比较
func (value *node) scalarEqual(expected *node) bool {
	if value.tag != expected.tag {
		return false
	}
	switch value.tag {
	case tagFloat:
		return math.Float32frombits(binary.BigEndian.Uint32(value.raw)) == math.Float32frombits(binary.BigEndian.Uint32(expected.raw))
	case tagDouble:
		return math.Float64frombits(binary.BigEndian.Uint64(value.raw)) == math.Float64frombits(binary.BigEndian.Uint64(expected.raw))
	default:
		return bytes.Equal(value.raw, expected.raw)
	}
}

// modifiedUTF8 将正常 UTF-8 字符串编码为 Java 的 UTF-16 单元序列，保留 NUL 和补充平面字符
func modifiedUTF8(text string) []byte {
	var output []byte
	for _, unit := range utf16.Encode([]rune(text)) {
		switch {
		case unit >= 1 && unit <= 0x7f:
			output = append(output, byte(unit))
		case unit <= 0x7ff:
			output = append(output, 0xc0|byte(unit>>6), 0x80|byte(unit&0x3f))
		default:
			output = append(output, 0xe0|byte(unit>>12), 0x80|byte(unit>>6&0x3f), 0x80|byte(unit&0x3f))
		}
	}
	return output
}
