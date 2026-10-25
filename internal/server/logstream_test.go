package server

import (
	"strings"
	"testing"
)

func TestSSELogEvent(t *testing.T) {
	got := sseLogEvent([]string{"первая", "вто\nрая", "третья\r"}, 0)
	want := "data: первая\ndata: вто рая\ndata: третья \n\n"
	if got != want {
		t.Fatalf("получили %q, ожидалось %q", got, want)
	}
	if !strings.HasSuffix(got, "\n\n") {
		t.Fatal("событие должно заканчиваться пустой строкой")
	}
	if g := sseLogEvent([]string{"a"}, 7); !strings.Contains(g, "пропущено строк: 7") {
		t.Fatalf("нет пометки о пропуске: %q", g)
	}
}
