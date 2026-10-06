package main

import (
	"io"
	"strings"
	"testing"
)

// 隐藏 Reader 的 WriterTo，确保实际触发 io.Copy 的 ReaderFrom/Write 选择。
type readOnly struct{ io.Reader }

func TestBoundedBufferCopyCannotBypassLimit(t *testing.T) {
	output := &boundedBuffer{limit: 8}
	if _, promoted := any(output).(io.ReaderFrom); promoted {
		t.Fatal("有界缓冲不能提升绕过 Write 的 ReadFrom")
	}
	_, err := io.Copy(output, readOnly{strings.NewReader("0123456789")})
	if err == nil || !output.exceeded || len(output.Bytes()) > output.limit {
		t.Fatal("真实 io.Copy 没有拒绝超限输出")
	}
}

func TestBoundedBufferAcceptsLimitAndRejectsNextByte(t *testing.T) {
	output := &boundedBuffer{limit: 8}
	if _, err := io.Copy(output, readOnly{strings.NewReader("01234567")}); err != nil {
		t.Fatal(err)
	}
	if string(output.Bytes()) != "01234567" || output.exceeded {
		t.Fatal("预算内输出没有保留")
	}
	if _, err := output.Write([]byte("8")); err == nil || !output.exceeded || string(output.Bytes()) != "01234567" {
		t.Fatal("超限写入改变了已有缓冲")
	}
}

func TestFailureTextCompatibilityKeepsStructuredSuccessRequired(t *testing.T) {
	result := object{"isError": true, "content": []any{object{"type": "text", "text": `{"code":"conflict","message":"The file changed"}`}}}
	output := decodeToolResult(result, "file_patch", true)
	if output["code"] != "conflict" {
		t.Fatal("错误文本协议兼容没有保留业务码")
	}
	defer func() {
		if recover() == nil {
			t.Fatal("成功缺少结构化响应时必须失败")
		}
	}()
	decodeToolResult(object{"content": result["content"]}, "file_patch", false)
}

func TestFailureTextRejectsOversizedInvalidUTF8AndWrongStatus(t *testing.T) {
	cases := []object{
		{"isError": true, "content": []any{object{"type": "text", "text": string([]byte{0xff})}}},
		{"isError": true, "content": []any{object{"type": "text", "text": string(make([]byte, 65537))}}},
		{"isError": false, "content": []any{object{"type": "text", "text": `{"code":"conflict"}`}}},
	}
	for _, result := range cases {
		func() {
			defer func() {
				if recover() == nil {
					t.Error("非法错误响应没有拒绝")
				}
			}()
			decodeToolResult(result, "file_patch", true)
		}()
	}
}
