// Package integration 转写校对模块端到端集成测试。
// 直连真实 MySQL（TEST_DB_* 环境变量可覆盖），在内存 Gin 引擎上调用真实路由，
// 每个用例独立准备数据并在结束后清理，可重复运行且结果一致。
// 运行：cd backend && go test ./internal/integration/ -v
package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/oralhistory/oralhistory/internal/config"
	"github.com/oralhistory/oralhistory/internal/constants"
	"github.com/oralhistory/oralhistory/internal/handler"
	"github.com/oralhistory/oralhistory/internal/middleware"
	"github.com/oralhistory/oralhistory/internal/model"
	"github.com/oralhistory/oralhistory/internal/repository"
	"github.com/oralhistory/oralhistory/internal/router"
	"github.com/oralhistory/oralhistory/internal/service"
	"github.com/oralhistory/oralhistory/internal/util"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

const jwtSecret = "integration-test-secret"

var envSeq int64

// testEnv 测试环境：真实数据库 + 内存 HTTP 引擎 + 数据夹具。
type testEnv struct {
	t          *testing.T
	db         *gorm.DB
	engine     *gin.Engine
	prefix     string
	userIDs    []uint
	projectIDs []uint
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		getEnv("TEST_DB_USER", "oralhistory_user"),
		getEnv("TEST_DB_PASSWORD", "oralhistory_pwd"),
		getEnv("TEST_DB_HOST", "127.0.0.1"),
		getEnv("TEST_DB_PORT", "10180"),
		getEnv("TEST_DB_NAME", "oralhistory_db"))
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	if err != nil {
		t.Skipf("测试数据库不可达，跳过集成测试: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil || sqlDB.Ping() != nil {
		t.Skipf("测试数据库不可达，跳过集成测试: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.Project{}, &model.Question{}, &model.Recording{},
		&model.TimelineMarker{}, &model.Transcript{}, &model.TranscriptSegment{}, &model.AuditLog{}); err != nil {
		t.Fatalf("迁移测试表结构失败: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{JWTSecret: jwtSecret, JWTExpireH: 72}

	userRepo := repository.NewUserRepository(db)
	projectRepo := repository.NewProjectRepository(db)
	recordingRepo := repository.NewRecordingRepository(db)
	transcriptRepo := repository.NewTranscriptRepository(db)
	auditRepo := repository.NewAuditLogRepository(db)

	userSvc := service.NewUserService(userRepo, cfg, logger)
	projectSvc := service.NewProjectService(projectRepo, logger)
	transcriptSvc := service.NewTranscriptService(transcriptRepo, recordingRepo, projectRepo, logger)
	auditSvc := service.NewAuditService(auditRepo, logger)

	userHandler := handler.NewUserHandler(userSvc, logger)
	projectHandler := handler.NewProjectHandler(projectSvc, auditSvc, logger)
	transcriptHandler := handler.NewTranscriptHandler(transcriptSvc, auditSvc, logger)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(middleware.ErrorHandler(logger), middleware.Recovery(logger))
	v1 := engine.Group("/api/v1")
	router.RegisterUserRoutes(v1, userHandler, cfg, logger)
	router.RegisterProjectRoutes(v1, projectHandler, cfg, logger)
	router.RegisterTranscriptRoutes(v1, transcriptHandler, cfg, logger)

	env := &testEnv{
		t:      t,
		db:     db,
		engine: engine,
		prefix: fmt.Sprintf("it%d", time.Now().UnixNano()%1_000_000_000+atomic.AddInt64(&envSeq, 1)),
	}
	t.Cleanup(env.cleanup)
	return env
}

// cleanup 仅删除本用例创建的数据（按项目与用户维度），保证重复运行结果一致。
func (e *testEnv) cleanup() {
	if len(e.projectIDs) > 0 {
		var transcriptIDs []uint
		e.db.Model(&model.Transcript{}).Where("project_id IN ?", e.projectIDs).Pluck("id", &transcriptIDs)
		if len(transcriptIDs) > 0 {
			e.db.Where("transcript_id IN ?", transcriptIDs).Delete(&model.TranscriptSegment{})
			e.db.Where("id IN ?", transcriptIDs).Delete(&model.Transcript{})
		}
		e.db.Where("project_id IN ?", e.projectIDs).Delete(&model.TimelineMarker{})
		e.db.Where("project_id IN ?", e.projectIDs).Delete(&model.Recording{})
		e.db.Where("project_id IN ?", e.projectIDs).Delete(&model.Question{})
		e.db.Where("id IN ?", e.projectIDs).Delete(&model.Project{})
	}
	if len(e.userIDs) > 0 {
		e.db.Where("user_id IN ?", e.userIDs).Delete(&model.AuditLog{})
		e.db.Where("id IN ?", e.userIDs).Delete(&model.User{})
	}
}

// ---------- 数据夹具 ----------

func (e *testEnv) uniq(word string) string {
	return fmt.Sprintf("%s_%s_%d", word, e.prefix, atomic.AddInt64(&envSeq, 1))
}

func (e *testEnv) newUser(role string) *model.User {
	e.t.Helper()
	hash, err := util.HashPassword("test_pwd_123")
	if err != nil {
		e.t.Fatalf("生成密码哈希失败: %v", err)
	}
	user := &model.User{
		Username:     e.uniq("user"),
		PasswordHash: hash,
		DisplayName:  "集成测试",
		Role:         role,
	}
	if err := e.db.Create(user).Error; err != nil {
		e.t.Fatalf("创建测试用户失败: %v", err)
	}
	e.userIDs = append(e.userIDs, user.ID)
	return user
}

func (e *testEnv) newProject(ownerID uint, status string) *model.Project {
	e.t.Helper()
	project := &model.Project{
		Title:           e.uniq("项目"),
		IntervieweeName: "受访者",
		BirthYear:       1940,
		Status:          status,
		CreatedBy:       ownerID,
	}
	if err := e.db.Create(project).Error; err != nil {
		e.t.Fatalf("创建测试项目失败: %v", err)
	}
	e.projectIDs = append(e.projectIDs, project.ID)
	return project
}

func (e *testEnv) newRecording(projectID uint, status string) *model.Recording {
	e.t.Helper()
	question := &model.Question{ProjectID: projectID, Content: e.uniq("问题")}
	if err := e.db.Create(question).Error; err != nil {
		e.t.Fatalf("创建测试问题失败: %v", err)
	}
	recording := &model.Recording{
		ProjectID:       projectID,
		QuestionID:      question.ID,
		DurationSeconds: 120,
		Status:          status,
		CreatedBy:       1,
	}
	if err := e.db.Create(recording).Error; err != nil {
		e.t.Fatalf("创建测试录音失败: %v", err)
	}
	return recording
}

func (e *testEnv) token(user *model.User) string {
	e.t.Helper()
	token, err := util.GenerateToken(user.ID, user.Username, user.Role, jwtSecret, 72)
	if err != nil {
		e.t.Fatalf("生成令牌失败: %v", err)
	}
	return token
}

// ---------- HTTP 与断言辅助 ----------

// req 发起 JSON 请求，返回 HTTP 状态码与解析后的响应体。
func (e *testEnv) req(method, path, token string, body any) (int, map[string]any) {
	e.t.Helper()
	code, raw := e.reqRaw(method, path, token, body)
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		e.t.Fatalf("响应不是合法 JSON: %s", string(raw))
	}
	return code, parsed
}

func (e *testEnv) reqRaw(method, path, token string, body any) (int, []byte) {
	e.t.Helper()
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			e.t.Fatalf("序列化请求体失败: %v", err)
		}
		reader = bytes.NewReader(buf)
	}
	req := httptest.NewRequest(method, "/api/v1"+path, reader)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	e.engine.ServeHTTP(recorder, req)
	return recorder.Code, recorder.Body.Bytes()
}

// expectStatus 断言 HTTP 状态码，失败时指出具体业务规则。
func (e *testEnv) expectStatus(rule string, got, want int) {
	e.t.Helper()
	if got != want {
		e.t.Errorf("业务规则[%s]: 期望 HTTP %d, 实际 %d", rule, want, got)
	}
}

// expectTrue 断言业务条件，失败时指出具体业务规则。
func (e *testEnv) expectTrue(rule string, cond bool, format string, args ...any) {
	e.t.Helper()
	if !cond {
		e.t.Errorf("业务规则[%s]: %s", rule, fmt.Sprintf(format, args...))
	}
}

// data 提取响应 data 字段。
func data(body map[string]any) map[string]any {
	if d, ok := body["data"].(map[string]any); ok {
		return d
	}
	return map[string]any{}
}

// dataList 提取响应 data.list 数组。
func dataList(body map[string]any) []any {
	if d, ok := body["data"].(map[string]any); ok {
		if l, ok := d["list"].([]any); ok {
			return l
		}
	}
	return nil
}

// segments 提取转写稿分段数组。
func segments(body map[string]any) []any {
	if segs, ok := data(body)["segments"].([]any); ok {
		return segs
	}
	return nil
}

func idOf(body map[string]any) uint {
	return uint(data(body)["id"].(float64))
}

func statusOf(body map[string]any) string {
	s, _ := data(body)["status"].(string)
	return s
}

// segBody 构造分段保存请求体。
func segBody(segs ...[4]any) map[string]any {
	list := make([]map[string]any, 0, len(segs))
	for _, s := range segs {
		list = append(list, map[string]any{
			"start_second": s[0], "end_second": s[1], "speaker": s[2], "content": s[3],
		})
	}
	return map[string]any{"segments": list}
}

// createDraft 快捷创建草稿并返回其 ID。
func (e *testEnv) createDraft(token string, recordingID, projectID uint) uint {
	e.t.Helper()
	code, body := e.req("POST", "/transcripts", token, map[string]any{"recording_id": recordingID, "project_id": projectID})
	if code != http.StatusOK {
		e.t.Fatalf("创建草稿失败: HTTP %d, %v", code, body)
	}
	return idOf(body)
}

// saveAndSubmit 保存分段并提交审核。
func (e *testEnv) saveAndSubmit(token string, transcriptID uint, segs ...[4]any) {
	e.t.Helper()
	code, body := e.req("PUT", fmt.Sprintf("/transcripts/%d/segments", transcriptID), token, segBody(segs...))
	if code != http.StatusOK {
		e.t.Fatalf("保存分段失败: HTTP %d, %v", code, body)
	}
	code, body = e.req("POST", fmt.Sprintf("/transcripts/%d/submit", transcriptID), token, nil)
	if code != http.StatusOK {
		e.t.Fatalf("提交审核失败: HTTP %d, %v", code, body)
	}
}

// approveAll 档案员确认全部分段并通过。
func (e *testEnv) approveAll(archivistToken string, transcriptID uint) {
	e.t.Helper()
	_, body := e.req("GET", fmt.Sprintf("/transcripts/%d", transcriptID), archivistToken, nil)
	for _, seg := range segments(body) {
		segID := uint(seg.(map[string]any)["id"].(float64))
		code, body := e.req("POST", fmt.Sprintf("/transcripts/%d/segments/%d/confirm", transcriptID, segID), archivistToken, nil)
		if code != http.StatusOK {
			e.t.Fatalf("确认分段 %d 失败: HTTP %d, %v", segID, code, body)
		}
	}
	code, body := e.req("POST", fmt.Sprintf("/transcripts/%d/approve", transcriptID), archivistToken, nil)
	if code != http.StatusOK {
		e.t.Fatalf("审核通过失败: HTTP %d, %v", code, body)
	}
}

var (
	roleInterviewer = constants.RoleInterviewer
	roleArchivist   = constants.RoleArchivist
	roleAdmin       = constants.RoleAdmin

	statusReady     = constants.RecordingStatusReady
	statusRecording = constants.RecordingStatusRecording

	projectInProgress = constants.ProjectStatusInProgress
	projectArchived   = constants.ProjectStatusArchived
)
