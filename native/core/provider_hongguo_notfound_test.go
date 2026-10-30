package core

import (
	"errors"
	"testing"
)

// 官网把「不存在的分类页 / 不存在的 series_id」统一回 HTTP 404：
// 前者是翻到总页数之外，应当按「没有更多」收尾；后者是这部剧确实不在官网，
// 应当换成可读提示。两条路径都靠这一个判断，所以边界要锁死。
func TestHongguoIsNotFound(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{errors.New("hongguoduanju.com HTTP 404"), true},
		{errors.New("hongguoduanju.com HTTP 404：访问被拒绝"), true},
		{errors.New("hongguoduanju.com HTTP 404: blocked"), true},
		{errors.New("hongguoduanju.com HTTP 4040"), false},
		{errors.New("hongguoduanju.com HTTP 403"), false},
		{errors.New("hongguoduanju.com HTTP 500"), false},
		{errors.New("红果 App 接口返回空内容"), false},
	}
	for _, item := range cases {
		if got := hongguoIsNotFound(item.err); got != item.want {
			t.Fatalf("hongguoIsNotFound(%v) = %v，期望 %v", item.err, got, item.want)
		}
	}
}
