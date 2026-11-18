// Package kotlin 是 Lemon Paperplane 的 Kotlin 插件：识别 Kotlin JVM 工程、顶层 fun main 主类，
// kotlin-application 运行配置（由 Gradle/Maven 编译并导出类路径，再用 java 直接启动）。
package kotlin

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	sdk "github.com/lemon-paperplane/lemon-paperplane-main/lemon-paperplane-main-plugin-sdk"
)

const typeApp = "kotlin-application"

// Plugin 实现 kotlin 插件。
type Plugin struct{}

// New 返回插件实例。
func New() any { return &Plugin{} }

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// project 是 gradle / maven 模型里的工程（只取本插件需要的字段）。
type project struct {
	Path          string   `json:"path"`
	Name          string   `json:"name"`
	Dir           string   `json:"dir"`
	Kotlin        bool     `json:"kotlin"`
	KotlinVersion string   `json:"kotlinVersion"`
	Android       bool     `json:"android"`
	SourceDirs    []string `json:"sourceDirs"`
}

type buildModel struct {
	Tool     string    `json:"tool"`
	RootName string    `json:"rootName"`
	Projects []project `json:"projects"`
}

// ---------------------------------------------------------------------------
// 识别

var kotlinMarkers = [][]byte{[]byte("org.jetbrains.kotlin"), []byte("kotlin(\""), []byte("kotlin-maven-plugin"), []byte("kotlin-stdlib"), []byte("kotlin-gradle-plugin")}

// Detect 在 Gradle / Maven 工程里找 Kotlin 插件、依赖或 src/main/kotlin。
func (p *Plugin) Detect(ctx *sdk.Context, d *sdk.DetectParams) (*sdk.DetectResult, error) {
	var dirs []string
	android := false
	for _, f := range d.Facets {
		if f.ID != "gradle" && f.ID != "maven" {
			continue
		}
		var data struct {
			Android  bool `json:"android"`
			Projects []struct {
				Dir string `json:"dir"`
			} `json:"projects"`
		}
		if b, err := json.Marshal(f.Data); err == nil {
			_ = json.Unmarshal(b, &data)
		}
		android = android || data.Android
		dirs = append(dirs, d.Dir)
		for _, pr := range data.Projects {
			dirs = append(dirs, filepath.Join(d.Dir, filepath.FromSlash(pr.Dir)))
		}
		dirs = append(dirs, filepath.Join(d.Dir, "gradle"))
	}
	for _, dir := range dirs {
		if sdk.DirExists(filepath.Join(dir, "src", "main", "kotlin")) {
			return found(android), nil
		}
		for _, f := range []string{"build.gradle.kts", "build.gradle", "pom.xml", "libs.versions.toml"} {
			b := []byte(sdk.ReadText(filepath.Join(dir, f), 512*1024))
			for _, m := range kotlinMarkers {
				if bytes.Contains(b, m) {
					return found(android), nil
				}
			}
		}
	}
	return &sdk.DetectResult{}, nil
}

func found(android bool) *sdk.DetectResult {
	f := sdk.Facet{ID: "kotlin"}
	if android {
		f.Data = map[string]any{"android": true}
	}
	return &sdk.DetectResult{Facets: []sdk.Facet{f}}
}

// ---------------------------------------------------------------------------
// 分析

// MainClass 是一个 Kotlin 主类。
type MainClass struct {
	Class   string `json:"class"`
	Project string `json:"project,omitempty"`
	File    string `json:"file"`
}

// Model 是 kotlin 插件的模型。
type Model struct {
	Version     string      `json:"version,omitempty"`
	MainClasses []MainClass `json:"mainClasses,omitempty"`
}

func readBuildModel(m *sdk.ModuleInfo) *buildModel {
	for _, id := range []string{"gradle", "maven"} {
		var bm buildModel
		if m.Model(id, &bm) && len(bm.Projects) > 0 {
			bm.Tool = id
			return &bm
		}
	}
	bm := &buildModel{Tool: "none"}
	for _, id := range []string{"gradle", "maven"} {
		f := m.Facet(id)
		if f == nil {
			continue
		}
		bm.Tool = id
		var data struct {
			Projects []struct {
				Path    string `json:"path"`
				Dir     string `json:"dir"`
				Android bool   `json:"android"`
			} `json:"projects"`
		}
		if b, err := json.Marshal(f.Data); err == nil {
			_ = json.Unmarshal(b, &data)
		}
		for _, pr := range data.Projects {
			bm.Projects = append(bm.Projects, project{Path: pr.Path, Dir: filepath.Join(m.Dir, filepath.FromSlash(pr.Dir)), Android: pr.Android})
		}
	}
	if len(bm.Projects) == 0 {
		bm.Projects = []project{{Dir: m.Dir}}
	}
	return bm
}

// Analyze 找顶层 fun main（以及 object 里的 @JvmStatic main），生成 kotlin-application 运行配置。
func (p *Plugin) Analyze(ctx *sdk.Context, a *sdk.AnalyzeParams) (*sdk.AnalyzeResult, error) {
	bm := readBuildModel(&a.Module)
	hasBoot := a.Module.HasFacet("spring-boot")
	model := Model{}
	for _, pr := range bm.Projects {
		if pr.KotlinVersion != "" && (model.Version == "" || sdk.CompareVersions(pr.KotlinVersion, model.Version) > 0) {
			model.Version = pr.KotlinVersion
		}
	}
	model.MainClasses = scanMains(bm, hasBoot)
	res := &sdk.AnalyzeResult{}
	if model.Version != "" {
		res.Info = []sdk.InfoItem{{Label: "Kotlin", Value: model.Version}}
	}
	names := map[string]int{}
	for _, mc := range model.MainClasses {
		names[simpleName(mc.Class)]++
	}
	var configs []sdk.RunConfiguration
	for _, mc := range model.MainClasses {
		name := simpleName(mc.Class)
		if names[name] > 1 && mc.Project != "" && mc.Project != ":" {
			name += " (" + strings.TrimPrefix(mc.Project, ":") + ")"
		}
		o := map[string]any{"mainClass": mc.Class}
		if mc.Project != "" && mc.Project != ":" {
			o["module"] = mc.Project
		}
		configs = append(configs, sdk.RunConfiguration{ID: sdk.StableID(typeApp, mc.Project, mc.Class), Name: name, Type: typeApp, Options: o, Source: "auto", Priority: 10})
	}
	// IDEA 的 Kotlin 运行配置
	for _, c := range a.IdeConfigs {
		if c.Type != "JetRunConfigurationType" || c.Options["MAIN_CLASS_NAME"] == "" {
			continue
		}
		main := c.Options["MAIN_CLASS_NAME"]
		o := map[string]any{"mainClass": main}
		if mod := ideaModule(c.Module, bm); mod != "" {
			o["module"] = mod
		}
		if v := c.Options["VM_PARAMETERS"]; v != "" {
			o["vmOptions"] = v
		}
		if v := c.Options["PROGRAM_PARAMETERS"]; v != "" {
			o["programArgs"] = v
		}
		kept := configs[:0]
		for _, x := range configs {
			if x.Options["mainClass"] != main {
				kept = append(kept, x)
			}
		}
		configs = append(kept, sdk.RunConfiguration{ID: sdk.StableID("idea", typeApp, c.Name, main), Name: c.Name, Type: typeApp, Options: o, Env: c.Env, Source: "idea", Priority: 20})
	}
	if len(configs) == 1 && !hasBoot {
		configs[0].Default = true
	}
	res.RunConfigurations = configs
	res.Model = mustJSON(model)
	return res, nil
}

var (
	packageRe     = regexp.MustCompile(`(?m)^\s*package\s+([\w.]+)`)
	topMainRe     = regexp.MustCompile(`(?m)^(?:public\s+|internal\s+)?(?:suspend\s+)?fun\s+main\s*\(`)
	jvmNameRe     = regexp.MustCompile(`@file:JvmName\(\s*"([\w$]+)"\s*\)`)
	objectMainRe  = regexp.MustCompile(`(?s)\bobject\s+(\w+)[^{]*\{.*?@JvmStatic\s+(?:public\s+)?fun\s+main\s*\(`)
	companionMain = regexp.MustCompile(`(?s)\bclass\s+(\w+).*?companion\s+object[^{]*\{.*?@JvmStatic\s+(?:public\s+)?fun\s+main\s*\(`)
)

// mainClassFromKotlin 返回 Kotlin 源文件里可运行的主类名（JVM 类名）。
func mainClassFromKotlin(data []byte, file string, skipBoot bool) string {
	if !bytes.Contains(data, []byte("main")) {
		return ""
	}
	if skipBoot && (bytes.Contains(data, []byte("@SpringBootApplication")) || bytes.Contains(data, []byte("runApplication<"))) {
		return ""
	}
	pkg := ""
	if m := packageRe.FindSubmatch(data); m != nil {
		pkg = string(m[1]) + "."
	}
	if topMainRe.Match(data) {
		if m := jvmNameRe.FindSubmatch(data); m != nil {
			return pkg + string(m[1])
		}
		return pkg + facadeName(strings.TrimSuffix(filepath.Base(file), ".kt"))
	}
	if m := objectMainRe.FindSubmatch(data); m != nil {
		return pkg + string(m[1])
	}
	if m := companionMain.FindSubmatch(data); m != nil {
		return pkg + string(m[1])
	}
	return ""
}

// facadeName 计算 Kotlin 文件门面类名：首字母大写、非标识符字符换成 _，再加 Kt（main.kt → MainKt）。
func facadeName(base string) string {
	var b strings.Builder
	for i, r := range base {
		ok := r == '_' || r == '$' || (r >= '0' && r <= '9' && i > 0) || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r > 127
		if !ok {
			r = '_'
		}
		if i == 0 && r >= 'a' && r <= 'z' {
			r = r - 'a' + 'A'
		}
		b.WriteRune(r)
	}
	return b.String() + "Kt"
}

func scanMains(bm *buildModel, skipBoot bool) []MainClass {
	var out []MainClass
	files := 0
	for _, pr := range bm.Projects {
		if pr.Android {
			continue
		}
		dirs := pr.SourceDirs
		if len(dirs) == 0 {
			dirs = []string{filepath.Join(pr.Dir, "src", "main", "kotlin"), filepath.Join(pr.Dir, "src", "main", "java")}
		}
		for _, sd := range dirs {
			if !sdk.DirExists(sd) {
				continue
			}
			_ = filepath.WalkDir(sd, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return nil
				}
				if d.IsDir() {
					if path != sd && (strings.HasPrefix(d.Name(), ".") || d.Name() == "test") {
						return fs.SkipDir
					}
					return nil
				}
				if !strings.HasSuffix(d.Name(), ".kt") {
					return nil
				}
				files++
				if files > 20000 || len(out) >= 50 {
					return fs.SkipAll
				}
				if st, err := d.Info(); err != nil || st.Size() > 1<<20 {
					return nil
				}
				data, err := os.ReadFile(path)
				if err != nil {
					return nil
				}
				if cls := mainClassFromKotlin(data, path, skipBoot); cls != "" {
					out = append(out, MainClass{Class: cls, Project: pr.Path, File: path})
				}
				return nil
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Class < out[j].Class })
	return out
}

func simpleName(fqcn string) string {
	if i := strings.LastIndexByte(fqcn, '.'); i >= 0 {
		return fqcn[i+1:]
	}
	return fqcn
}

func ideaModule(module string, bm *buildModel) string {
	if module == "" {
		return ""
	}
	if bm.Tool != "gradle" {
		return module
	}
	for _, suf := range []string{".main", ".test"} {
		module = strings.TrimSuffix(module, suf)
	}
	if module == bm.RootName {
		return ""
	}
	return ":" + strings.ReplaceAll(strings.TrimPrefix(module, bm.RootName+"."), ".", ":")
}

// ---------------------------------------------------------------------------
// 运行

type classpathResult struct {
	Classpath  []string `json:"classpath"`
	WorkingDir string   `json:"workingDir,omitempty"`
}

// PrepareRun：构建插件编译并导出类路径，java 插件生成启动命令。
func (p *Plugin) PrepareRun(ctx *sdk.Context, rp *sdk.PrepareRunParams) (*sdk.LaunchSpec, error) {
	if rp.Config.Type != typeApp {
		return nil, sdk.ErrNotSupported
	}
	opts := rp.Config.Options
	main := sdk.OptString(opts, "mainClass", "")
	if main == "" {
		return nil, sdk.UserError("%s", ctx.T("err.noMainClass"))
	}
	jdk, ok := rp.Toolchains["jdk"]
	if !ok {
		return nil, sdk.UserError("%s", ctx.T("err.noJdk"))
	}
	tool := ""
	switch {
	case rp.Module.HasFacet("gradle"):
		tool = "gradle"
	case rp.Module.HasFacet("maven"):
		tool = "maven"
	default:
		return nil, sdk.UserError("%s", ctx.T("err.noBuildTool"))
	}
	var cp classpathResult
	req := map[string]any{"project": rp.Project, "module": rp.Module, "toolchains": rp.Toolchains, "env": rp.Env, "runId": rp.RunID, "subproject": sdk.OptString(opts, "module", "")}
	if err := ctx.Host.Call(ctx, tool, "prepareClasspath", req, &cp); err != nil {
		return nil, err
	}
	cwd := sdk.OptString(opts, "workingDir", "")
	if cwd != "" && !filepath.IsAbs(cwd) {
		cwd = filepath.Join(rp.Project.Root, cwd)
	}
	if cwd == "" {
		cwd = cp.WorkingDir
	}
	if cwd == "" {
		cwd = rp.Module.Dir
	}
	var spec sdk.LaunchSpec
	err := ctx.Host.Call(ctx, "java", "buildCommand", map[string]any{
		"jdk": jdk, "classpath": cp.Classpath, "mainClass": main, "cwd": cwd,
		"vmOptions": sdk.OptString(opts, "vmOptions", ""), "programArgs": sdk.OptString(opts, "programArgs", ""),
		"key": rp.Config.ID,
	}, &spec)
	if err != nil {
		return nil, err
	}
	return &spec, nil
}
