// 本文件验证物品载荷的规范化、不变量、资源边界与 Java 字符串互操作

package itemdata

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os"
	"testing"
)

// TestNeoForgeItemFixtures 验证真实 Mod 导出的单件物品与中心规范化和匹配规则兼容
func TestNeoForgeItemFixtures(t *testing.T) {
	hashes := make(map[[32]byte]string)
	for _, fixture := range []struct{ name, itemID string }{
		{"stone", "minecraft:stone"}, {"named", "minecraft:stone"},
		{"damaged", "minecraft:diamond_sword"}, {"nested", "minecraft:stone"},
		{"enchanted", "minecraft:diamond_sword"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			input, err := os.ReadFile("testdata/neoforge-1.21.1/" + fixture.name + ".nbt")
			if err != nil {
				t.Fatal(err)
			}
			document, err := Parse(fixture.itemID, input)
			if err != nil {
				t.Fatal(err)
			}
			if previous, exists := hashes[document.Hash]; exists {
				t.Fatalf("%s incorrectly merges with %s", fixture.name, previous)
			}
			hashes[document.Hash] = fixture.name
			reparsed, err := Parse(fixture.itemID, document.Canonical)
			if err != nil || !bytes.Equal(document.Canonical, reparsed.Canonical) {
				t.Fatal("unstable canonical Mod payload", err)
			}
			if fixture.name == "nested" {
				rule := Rule{Conditions: []Condition{{Path: []PathPart{testKey("components"), testKey("minecraft:custom_data"), testKey("child"), testKey("count")}, Operator: OperatorTypedEquals, Type: "int", Value: []byte("17")}}}
				if matched, err := document.Matches(rule); err != nil || !matched {
					t.Fatal("nested count was lost", err)
				}
			}
			if fixture.name == "enchanted" {
				rule := Rule{Conditions: []Condition{{
					Path:     []PathPart{testKey("components"), testKey("minecraft:enchantments"), testKey("levels"), testKey("minecraft:sharpness")},
					Operator: OperatorTypedEquals, Type: "int", Value: []byte("3"),
				}}}
				if matched, err := document.Matches(rule); err != nil || !matched {
					t.Fatal("sharpness III integer level was lost", err)
				}
				rule.Conditions[0].Type = "short"
				if matched, err := document.Matches(rule); err != nil || matched {
					t.Fatal("enchantment level numeric type was coerced", err)
				}
			}
		})
	}
}

// TestCanonicalItemData 确认仅键顺序可忽略，标签类型、列表顺序与所有叶值必须保留
func TestCanonicalItemData(t *testing.T) {
	id := testField(tagString, "id", testString("minecraft:stone"))
	integer := testField(tagInt, "number", []byte{0, 0, 0, 1})
	text := testField(tagString, "name", testString("物品"))
	list := testField(tagList, "list", []byte{tagByte, 0, 0, 0, 2, 1, 2})
	compound := testField(tagCompound, "components", testCompound(integer, text, list))
	first := testDocument(t, testRoot(id, compound))
	reordered := testField(tagCompound, "components", testCompound(list, text, integer))
	second := testDocument(t, testRoot(reordered, id))
	if !bytes.Equal(first.Canonical, second.Canonical) || first.Hash != second.Hash {
		t.Fatal("compound key order changed canonical identity")
	}
	if reparsed := testDocument(t, first.Canonical); !bytes.Equal(first.Canonical, reparsed.Canonical) {
		t.Fatal("canonicalization is not idempotent")
	}
	for name, changed := range map[string][]byte{
		"numeric type": testCompound(testField(tagShort, "number", []byte{0, 1}), text, list),
		"list order":   testCompound(integer, text, testField(tagList, "list", []byte{tagByte, 0, 0, 0, 2, 2, 1})),
	} {
		t.Run(name, func(t *testing.T) {
			variant := testField(tagCompound, "components", changed)
			if first.Hash == testDocument(t, testRoot(id, variant)).Hash {
				t.Fatal("distinct type or order was merged")
			}
		})
	}

	// 覆盖所有叶值和空列表，确认原始数值位型、数组顺序及空列表类型没有丢失
	allTags := [][]byte{
		testField(tagByte, "a", []byte{0xff}),
		testField(tagShort, "b", []byte{0xff, 0xfe}),
		testField(tagInt, "c", []byte{0, 0, 1, 2}),
		testField(tagLong, "d", []byte{0xff, 0, 0, 0, 0, 0, 0, 1}),
		testField(tagFloat, "e", []byte{0x7f, 0xc0, 0, 1}),
		testField(tagDouble, "f", []byte{0x80, 0, 0, 0, 0, 0, 0, 0}),
		testField(tagByteArray, "g", []byte{0, 0, 0, 2, 0, 0xff}),
		testField(tagString, "h", testString("")),
		id,
		testField(tagList, "j", []byte{tagLong, 0, 0, 0, 0}),
		testField(tagCompound, "k", []byte{tagEnd}),
		testField(tagIntArray, "l", []byte{0, 0, 0, 1, 0, 0, 0, 2}),
		testField(tagLongArray, "m", []byte{0, 0, 0, 1, 0xff, 0, 0, 0, 0, 0, 0, 2}),
	}
	input := testRoot(allTags...)
	if !bytes.Equal(input, testDocument(t, input).Canonical) {
		t.Fatal("canonicalization changed a sorted payload")
	}
	before := testDocument(t, input)
	clear(input)
	matched, err := before.Matches(Rule{Conditions: []Condition{{Path: []PathPart{testKey("a")}, Operator: OperatorTypedEquals, Type: "byte", Value: []byte("-1")}}})
	if err != nil || !matched {
		t.Fatal("caller buffer mutation changed parsed document", err)
	}
}

// TestNBTBoundary 验证伪造长度、重复键、截断、深度和节点上限不会进入无界解码
func TestNBTBoundary(t *testing.T) {
	id := testField(tagString, "id", testString("minecraft:stone"))
	for name, data := range map[string][]byte{
		"duplicate key":  testRoot(id, id),
		"count present":  testRoot(id, testField(tagByte, "count", []byte{1})),
		"trailing data":  append(testRoot(id), 0),
		"wrong root":     {tagList, 0, 0},
		"named root":     {tagCompound, 0, 1, 'a', tagEnd},
		"unknown tag":    testRoot(id, testField(13, "x", nil)),
		"negative size":  testRoot(id, testField(tagList, "x", []byte{tagByte, 0xff, 0xff, 0xff, 0xff})),
		"huge array":     testRoot(id, testField(tagLongArray, "x", []byte{0x7f, 0xff, 0xff, 0xff})),
		"end in list":    testRoot(id, testField(tagList, "x", []byte{tagEnd, 0, 0, 0, 1, 0})),
		"empty bad list": testRoot(id, testField(tagList, "x", []byte{13, 0, 0, 0, 0})),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse("minecraft:stone", data); !errors.Is(err, ErrMalformed) {
				t.Fatalf("got %v, want malformed", err)
			}
		})
	}
	valid := testRoot(id)
	for end := range len(valid) {
		if _, err := Parse("minecraft:stone", valid[:end]); err == nil {
			t.Fatalf("accepted truncated input at %d", end)
		}
	}
	for _, itemID := range []string{"stone", "Minecraft:stone", "minecraft:stone:extra", "minecraft:", "minecraft:/石"} {
		if _, err := Parse(itemID, valid); !errors.Is(err, ErrItemID) {
			t.Fatalf("accepted invalid identifier %q: %v", itemID, err)
		}
	}
	if _, err := Parse("minecraft:dirt", valid); !errors.Is(err, ErrItemID) {
		t.Fatal("accepted mismatched identifier", err)
	}
	if _, err := Parse("minecraft:stone", make([]byte, MaxPayload+1)); !errors.Is(err, ErrLimit) {
		t.Fatal("accepted oversized payload", err)
	}
	for _, count := range []int{MaxNodes - 3, MaxNodes - 2} {
		list := append([]byte{tagByte}, binary.BigEndian.AppendUint32(nil, uint32(count))...)
		list = append(list, make([]byte, count)...)
		_, err := Parse("minecraft:stone", testRoot(id, testField(tagList, "x", list)))
		if count == MaxNodes-3 && err != nil || count == MaxNodes-2 && !errors.Is(err, ErrLimit) {
			t.Fatalf("node boundary %d: %v", count, err)
		}
	}
	for _, depth := range []int{MaxDepth, MaxDepth + 1} {
		nested := testField(tagByte, "x", []byte{1})
		for current := 2; current < depth; current++ {
			nested = testField(tagCompound, "nested", testCompound(nested))
		}
		_, err := Parse("minecraft:stone", testRoot(id, nested))
		if depth == MaxDepth && err != nil || depth > MaxDepth && !errors.Is(err, ErrLimit) {
			t.Fatalf("depth boundary %d: %v", depth, err)
		}
	}
}

// TestJavaModifiedUTF8 使用 Java 21 DataOutputStream.writeUTF 生成的载荷核对互操作
func TestJavaModifiedUTF8(t *testing.T) {
	const javaHex = "0a00000800026964000f6d696e6563726166743a73746f6e65080011636f6d706f6e656e74c080eda0bdedb880000c74657874c080eda0bdedb88000"
	data, err := hex.DecodeString(javaHex)
	if err != nil {
		t.Fatal(err)
	}
	document := testDocument(t, data)
	matched, err := document.Matches(Rule{Conditions: []Condition{{
		Path: []PathPart{testKey("component\x00😀")}, Operator: OperatorTypedEquals,
		Type: "string", Value: []byte(`"text\u0000😀"`),
	}}})
	if err != nil || !matched {
		t.Fatal("Java NUL/non-BMP string did not match", err)
	}
	for _, invalid := range [][]byte{{0}, {0xc0, 0x81}, {0xc1, 0x80}, {0xc2}, {0xe0, 0x80, 0x80}, {0xed, 0xa0}, {0xf0, 0x9f, 0x98, 0x80}, {0x80}} {
		if validModifiedUTF8(invalid) {
			t.Fatalf("accepted invalid Java string %x", invalid)
		}
	}
	for _, valid := range [][]byte{{0xc0, 0x80}, {0xed, 0xa0, 0x80}, {0xed, 0xb0, 0x80}, {0xdf, 0xbf}, {0xef, 0xbf, 0xbf}} {
		if !validModifiedUTF8(valid) {
			t.Fatalf("rejected Java UTF-16 unit %x", valid)
		}
	}
}

// FuzzParse 验证任意输入不会崩溃，成功输入规范化后仍可稳定解析
func FuzzParse(f *testing.F) {
	f.Add(testRoot(testField(tagString, "id", testString("minecraft:stone"))))
	f.Add([]byte{tagCompound, 0, 0, tagList, 0, 1, 'x', tagByte, 0x7f, 0xff, 0xff, 0xff})
	f.Fuzz(func(t *testing.T, input []byte) {
		document, err := Parse("minecraft:stone", input)
		if err != nil {
			return
		}
		reparsed, err := Parse("minecraft:stone", document.Canonical)
		if err != nil || !bytes.Equal(document.Canonical, reparsed.Canonical) {
			t.Fatal("canonical payload cannot be parsed stably", err)
		}
	})
}

// testDocument 解析测试载荷并在失败时停止当前用例
func testDocument(t *testing.T, data []byte) *Document {
	t.Helper()
	document, err := Parse("minecraft:stone", data)
	if err != nil {
		t.Fatal(err)
	}
	return document
}

// testString 生成普通字符串的 NBT 长度前缀，测试数据使用 Java modified UTF-8
func testString(value string) []byte {
	data := modifiedUTF8(value)
	return append(binary.BigEndian.AppendUint16(nil, uint16(len(data))), data...)
}

// testField 生成一个带类型和名称的完整 NBT 字段
func testField(tag byte, name string, payload []byte) []byte {
	output := append([]byte{tag}, testString(name)...)
	return append(output, payload...)
}

// testCompound 顺序拼接测试字段并添加结束标签
func testCompound(fields ...[]byte) []byte {
	return append(bytes.Join(fields, nil), tagEnd)
}

// testRoot 封装 Java NbtIo 的空名 compound 根标签
func testRoot(fields ...[]byte) []byte {
	return append([]byte{tagCompound, 0, 0}, testCompound(fields...)...)
}

// testKey 创建指向完整 compound 键名的结构化路径段
func testKey(key string) PathPart {
	return PathPart{Key: &key}
}
