package kotlin

import (
	"strings"
	"testing"

	sdk "github.com/lemon-paperplane/lemon-paperplane-main/lemon-paperplane-main-plugin-sdk"
)

func TestScanMains(t *testing.T) {
	bm := &buildModel{Projects: []project{{Path: ":", Dir: "testdata/app"}}}
	got := scanMains(bm, true)
	var names []string
	for _, m := range got {
		names = append(names, m.Class)
	}
	if strings.Join(names, ",") != "com.acme.CliRunner,com.acme.MainKt,com.acme.Server" {
		t.Errorf("mains %v", names)
	}
	// 没有 spring-boot 插件时 Boot 启动类也作为普通 Kotlin 应用
	if all := scanMains(bm, false); len(all) != 4 {
		t.Errorf("without boot: %d", len(all))
	}
	if facadeName("app-main") != "App_mainKt" {
		t.Error(facadeName("app-main"))
	}
}

func TestDetect(t *testing.T) {
	p := &Plugin{}
	r, _ := p.Detect(&sdk.Context{}, &sdk.DetectParams{Dir: "testdata/app", Facets: []sdk.Facet{{ID: "gradle", Data: map[string]any{"projects": []any{map[string]any{"path": ":", "dir": ""}}}}}})
	if len(r.Facets) != 1 || r.Facets[0].ID != "kotlin" {
		t.Errorf("detect %+v", r)
	}
	r, _ = p.Detect(&sdk.Context{}, &sdk.DetectParams{Dir: "testdata/app"})
	if len(r.Facets) != 0 {
		t.Error("needs gradle/maven facet")
	}
}

func TestIdeaModule(t *testing.T) {
	bm := &buildModel{Tool: "gradle", RootName: "root"}
	if m := ideaModule("root.server.main", bm); m != ":server" {
		t.Error(m)
	}
}
