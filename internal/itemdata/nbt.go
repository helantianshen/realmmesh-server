// 本文件负责仓库单件物品 NBT 的有界解析、完整规范化与摘要计算

// Package itemdata 保留物品 NBT 的原始类型和数据，并提供有限名单匹配
package itemdata

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"slices"
	"strings"
)

const (
	MaxPayload = 1 << 20 // 未压缩 NBT 的最大字节数，超过 1 MiB 时拒绝解析
	MaxDepth   = 64      // 标签最大嵌套深度，根标签计为第 1 层
	MaxNodes   = 65536   // 最多解析的标签数，列表元素计入，数组作为一个标签
)

const (
	tagEnd byte = iota
	tagByte
	tagShort
	tagInt
	tagLong
	tagFloat
	tagDouble
	tagByteArray
	tagString
	tagList
	tagCompound
	tagIntArray
	tagLongArray
)

// Document 保存通过校验的单件载荷，Canonical 和 Hash 不应由调用方修改
type Document struct {
	ItemID    string
	Canonical []byte
	Hash      [sha256.Size]byte
	root      *node
}

type node struct {
	tag      byte
	raw      []byte
	fields   []field
	elements []*node
}

type field struct {
	name string
	data *node
}

type decoder struct {
	data  []byte
	pos   int
	nodes int
}

// Parse 校验单件 NbtIo 载荷和注册 ID，仅对 compound 键排序后计算 SHA-256
func Parse(itemID string, data []byte) (*Document, error) {
	if !validItemID(itemID) {
		return nil, ErrItemID
	}
	if len(data) > MaxPayload {
		return nil, ErrLimit
	}
	if len(data) < 3 || data[0] != tagCompound || data[1] != 0 || data[2] != 0 {
		return nil, fmt.Errorf("%w: root must be an unnamed compound", ErrMalformed)
	}

	// 树中的叶值引用独立输入副本，调用方复用请求缓冲区不会改变名单匹配结果
	parser := decoder{data: bytes.Clone(data), pos: 3}
	root, err := parser.readNode(tagCompound, 1)
	if err != nil {
		return nil, err
	}
	if parser.pos != len(parser.data) {
		return nil, fmt.Errorf("%w: trailing bytes", ErrMalformed)
	}
	id := root.child("id")
	if id == nil || id.tag != tagString || string(id.raw[2:]) != itemID {
		return nil, ErrItemID
	}
	if root.child("count") != nil {
		return nil, fmt.Errorf("%w: single-item payload must omit count", ErrMalformed)
	}

	canonical := make([]byte, 3, len(data))
	canonical[0] = tagCompound
	canonical = root.appendTo(canonical)
	return &Document{ItemID: itemID, Canonical: canonical, Hash: sha256.Sum256(canonical), root: root}, nil
}

// readNode 在递归和分配前检查标签、深度与节点预算，保留叶值的原始编码
func (parser *decoder) readNode(tag byte, depth int) (*node, error) {
	if depth > MaxDepth || parser.nodes >= MaxNodes {
		return nil, ErrLimit
	}
	if tag < tagByte || tag > tagLongArray {
		return nil, fmt.Errorf("%w: unsupported tag", ErrMalformed)
	}
	parser.nodes++
	value := &node{tag: tag}
	start := parser.pos
	var err error
	switch tag {
	case tagByte:
		_, err = parser.take(1)
	case tagShort:
		_, err = parser.take(2)
	case tagInt, tagFloat:
		_, err = parser.take(4)
	case tagLong, tagDouble:
		_, err = parser.take(8)
	case tagString:
		_, err = parser.readString()
	case tagByteArray, tagIntArray, tagLongArray:
		err = parser.readArray(tag)
	case tagList:
		err = parser.readList(value, depth)
	case tagCompound:
		err = parser.readCompound(value, depth)
	}
	if err != nil {
		return nil, err
	}
	if tag != tagCompound && tag != tagList {
		value.raw = parser.data[start:parser.pos]
	}
	return value, nil
}

// readArray 按剩余字节验证数组长度，不按外部声明的长度分配数组
func (parser *decoder) readArray(tag byte) error {
	length, err := parser.readLength()
	if err != nil {
		return err
	}
	width := 1
	if tag == tagIntArray {
		width = 4
	} else if tag == tagLongArray {
		width = 8
	}
	if length > (len(parser.data)-parser.pos)/width {
		return fmt.Errorf("%w: truncated array", ErrMalformed)
	}
	_, err = parser.take(length * width)
	return err
}

// readList 保留元素类型和顺序，并在分配前拒绝超出节点或字节预算的长度
func (parser *decoder) readList(value *node, depth int) error {
	header, err := parser.take(1)
	if err != nil {
		return err
	}
	elementTag := header[0]
	length, err := parser.readLength()
	if err != nil {
		return err
	}
	if elementTag > tagLongArray || elementTag == tagEnd && length != 0 {
		return fmt.Errorf("%w: invalid list element type", ErrMalformed)
	}
	if length > MaxNodes-parser.nodes {
		return ErrLimit
	}
	if length > len(parser.data)-parser.pos {
		return fmt.Errorf("%w: truncated list", ErrMalformed)
	}
	value.raw = parser.data[parser.pos-5 : parser.pos]
	value.elements = make([]*node, length)
	for index := range value.elements {
		value.elements[index], err = parser.readNode(elementTag, depth+1)
		if err != nil {
			return err
		}
	}
	return nil
}

// readCompound 拒绝重复键，防止同一载荷在游戏服与中心产生不同解释
func (parser *decoder) readCompound(value *node, depth int) error {
	names := make(map[string]struct{})
	for {
		header, err := parser.take(1)
		if err != nil {
			return err
		}
		if header[0] == tagEnd {
			break
		}
		name, err := parser.readString()
		if err != nil {
			return err
		}
		if _, exists := names[name]; exists {
			return fmt.Errorf("%w: duplicate compound key", ErrMalformed)
		}
		names[name] = struct{}{}
		child, err := parser.readNode(header[0], depth+1)
		if err != nil {
			return err
		}
		value.fields = append(value.fields, field{name: name, data: child})
	}
	slices.SortFunc(value.fields, func(left, right field) int {
		return strings.Compare(left.name, right.name)
	})
	return nil
}

// readLength 读取有符号 32 位长度，负数不能进入切片和内存分配
func (parser *decoder) readLength() (int, error) {
	data, err := parser.take(4)
	if err != nil {
		return 0, err
	}
	length := int32(binary.BigEndian.Uint32(data))
	if length < 0 {
		return 0, fmt.Errorf("%w: negative length", ErrMalformed)
	}
	return int(length), nil
}

// readString 校验 Java modified UTF-8，并返回原始编码以保留 UTF-16 代理项
func (parser *decoder) readString() (string, error) {
	header, err := parser.take(2)
	if err != nil {
		return "", err
	}
	data, err := parser.take(int(binary.BigEndian.Uint16(header)))
	if err != nil {
		return "", err
	}
	if !validModifiedUTF8(data) {
		return "", fmt.Errorf("%w: invalid modified UTF-8", ErrMalformed)
	}
	return string(data), nil
}

// take 返回输入中的完整区间，不允许溢出、越界或读取截断数据
func (parser *decoder) take(length int) ([]byte, error) {
	if length < 0 || length > len(parser.data)-parser.pos {
		return nil, fmt.Errorf("%w: truncated payload", ErrMalformed)
	}
	data := parser.data[parser.pos : parser.pos+length]
	parser.pos += length
	return data, nil
}

// appendTo 按已排序的 compound 字段重建载荷，其他编码与列表顺序原样保留
func (value *node) appendTo(output []byte) []byte {
	switch value.tag {
	case tagCompound:
		for _, member := range value.fields {
			output = append(output, member.data.tag)
			output = binary.BigEndian.AppendUint16(output, uint16(len(member.name)))
			output = append(output, member.name...)
			output = member.data.appendTo(output)
		}
		return append(output, tagEnd)
	case tagList:
		output = append(output, value.raw...)
		for _, child := range value.elements {
			output = child.appendTo(output)
		}
		return output
	default:
		return append(output, value.raw...)
	}
}

// child 从已排序的 compound 中查找完整键名，非 compound 不存在命中结果
func (value *node) child(name string) *node {
	if value.tag != tagCompound {
		return nil
	}
	index, found := slices.BinarySearchFunc(value.fields, name, func(member field, target string) int {
		return strings.Compare(member.name, target)
	})
	if !found {
		return nil
	}
	return value.fields[index].data
}

// validItemID 校验带 namespace 的完整注册 ID，不接受空段或隐式默认 namespace
func validItemID(itemID string) bool {
	namespace, path, found := strings.Cut(itemID, ":")
	if !found || namespace == "" || path == "" {
		return false
	}
	for index := range itemID {
		character := itemID[index]
		if index == len(namespace) || character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '_' || character == '-' || character == '.' {
			continue
		}
		if character != '/' || index < len(namespace) {
			return false
		}
	}
	return true
}

// validModifiedUTF8 接受 DataOutput.writeUTF 的编码，拒绝标准四字节 UTF-8 与非规范长编码
func validModifiedUTF8(data []byte) bool {
	for index := 0; index < len(data); {
		first := data[index]
		switch {
		case first >= 1 && first <= 0x7f:
			index++
		case first >= 0xc0 && first <= 0xdf:
			if index+1 >= len(data) || data[index+1]&0xc0 != 0x80 {
				return false
			}
			value := uint16(first&0x1f)<<6 | uint16(data[index+1]&0x3f)
			if value != 0 && value < 0x80 {
				return false
			}
			index += 2
		case first >= 0xe0 && first <= 0xef:
			if index+2 >= len(data) || data[index+1]&0xc0 != 0x80 || data[index+2]&0xc0 != 0x80 {
				return false
			}
			value := uint16(first&0x0f)<<12 | uint16(data[index+1]&0x3f)<<6 | uint16(data[index+2]&0x3f)
			if value < 0x800 {
				return false
			}
			index += 3
		default:
			return false
		}
	}
	return true
}
