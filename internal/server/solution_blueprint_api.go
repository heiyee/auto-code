package server

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const defaultSolutionTemplateID = "saas-ticket-knowledge"

type v1SolutionTemplate struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Summary          string `json:"summary"`
	BusinessGoal     string `json:"businessGoal"`
	SuggestedStack   string `json:"suggestedStack"`
	RequirementCount int    `json:"requirementCount"`
}

type v1SolutionBootstrapInput struct {
	AppName          string `json:"appName"`
	TemplateID       string `json:"templateId"`
	Repository       string `json:"repository"`
	Branch           string `json:"branch"`
	WorkDir          string `json:"workDir"`
	CLIType          string `json:"cliType"`
	AutoClearSession *bool  `json:"autoClearSession"`
}

type v1SolutionBootstrapData struct {
	Project      v1Project          `json:"project"`
	Template     v1SolutionTemplate `json:"template"`
	Requirements []v1Requirement    `json:"requirements"`
	DesignBrief  string             `json:"designBrief"`
	AutoProgress bool               `json:"autoProgress"`
}

type solutionBlueprint struct {
	template     v1SolutionTemplate
	designBrief  string
	requirements []solutionRequirementSpec
}

type solutionRequirementSpec struct {
	title       string
	objective   string
	deliverable []string
	acceptance  []string
}

func (a *App) handleAPIV1SolutionTemplates(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	catalog := builtinSolutionBlueprintCatalog()
	templates := make([]v1SolutionTemplate, 0, len(catalog))
	for _, item := range catalog {
		templates = append(templates, item.template)
	}
	sort.SliceStable(templates, func(i, j int) bool {
		return templates[i].ID < templates[j].ID
	})
	writeV1Data(w, http.StatusOK, templates)
}

func (a *App) handleAPIV1SolutionBootstrap(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var input v1SolutionBootstrapInput
	if err := decodeJSONBody(r, &input); err != nil {
		writeV1Error(w, http.StatusBadRequest, err.Error())
		return
	}

	appName := strings.TrimSpace(input.AppName)
	if appName == "" {
		writeV1Error(w, http.StatusBadRequest, "appName is required")
		return
	}

	catalog := builtinSolutionBlueprintCatalog()
	templateID := strings.TrimSpace(input.TemplateID)
	if templateID == "" {
		templateID = defaultSolutionTemplateID
	}
	blueprint, ok := catalog[templateID]
	if !ok {
		writeV1Error(w, http.StatusBadRequest, "unsupported templateId")
		return
	}

	branch := strings.TrimSpace(input.Branch)
	if branch == "" {
		branch = "main"
	}

	slug := slugifyAppName(appName)
	repository := strings.TrimSpace(input.Repository)
	if repository == "" {
		repository = "local://" + slug
	}

	workDir := strings.TrimSpace(input.WorkDir)
	if workDir == "" {
		workDir = filepath.Join("workspace", slug)
	}

	cliType := a.resolveBootstrapCLIType(input.CLIType)
	autoClearSession := true
	if input.AutoClearSession != nil {
		autoClearSession = *input.AutoClearSession
	}

	project, err := a.projectSvc.Create(ProjectMutation{
		Name:       appName,
		Repository: repository,
		Branch:     branch,
		WorkDir:    workDir,
	})
	if err != nil {
		writeV1Error(w, http.StatusBadRequest, err.Error())
		return
	}

	rollback := func() {
		a.shutdownProjectCLIRuntimes(project.ID)
		_, _ = a.projectSvc.Delete(project.ID)
	}

	if err := initializeBootstrapWorkspace(*project, blueprint, appName); err != nil {
		rollback()
		writeV1Error(w, http.StatusBadRequest, err.Error())
		return
	}

	createdRequirements := make([]Requirement, 0, len(blueprint.requirements))
	for idx, spec := range blueprint.requirements {
		requirement, createErr := a.requirementSvc.Create(RequirementMutation{
			ProjectID:        project.ID,
			Title:            fmt.Sprintf("阶段 %d: %s", idx+1, spec.title),
			Description:      renderSolutionRequirementPrompt(appName, spec),
			ExecutionMode:    RequirementExecutionModeAuto,
			CLIType:          cliType,
			AutoClearSession: autoClearSession,
		})
		if createErr != nil {
			rollback()
			writeV1Error(w, http.StatusBadRequest, createErr.Error())
			return
		}
		createdRequirements = append(createdRequirements, *requirement)
	}

	if a.requirementAuto != nil {
		a.requirementAuto.SyncProject(project.ID, "")
	}

	requirements, err := a.requirementSvc.ListByProject(project.ID)
	if err != nil {
		rollback()
		writeV1Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	requirements = sortRequirementsForAutomation(requirements)
	resultRequirements := make([]v1Requirement, 0, len(requirements))
	for _, item := range requirements {
		resultRequirements = append(resultRequirements, a.toV1Requirement(item))
	}

	reloadedProject, err := a.projectSvc.Get(project.ID)
	if err != nil {
		rollback()
		writeV1Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeV1Data(w, http.StatusCreated, v1SolutionBootstrapData{
		Project:      toV1Project(*reloadedProject),
		Template:     blueprint.template,
		Requirements: resultRequirements,
		DesignBrief:  blueprint.designBrief,
		AutoProgress: true,
	})
}

func (a *App) resolveBootstrapCLIType(raw string) string {
	selected := strings.TrimSpace(strings.ToLower(raw))
	types := a.cliSessionSvc.Types()
	if len(types) == 0 {
		return selected
	}
	available := make(map[string]struct{}, len(types))
	for _, item := range types {
		normalized := strings.TrimSpace(strings.ToLower(item))
		if normalized == "" {
			continue
		}
		available[normalized] = struct{}{}
	}
	if selected != "" {
		if _, ok := available[selected]; ok {
			return selected
		}
	}
	for _, preferred := range []string{"codex", "claude", "cursor"} {
		if _, ok := available[preferred]; ok {
			return preferred
		}
	}
	return strings.TrimSpace(strings.ToLower(types[0]))
}

func initializeBootstrapWorkspace(project Project, blueprint solutionBlueprint, appName string) error {
	root := strings.TrimSpace(project.WorkDir)
	if root == "" {
		return nil
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("prepare project workdir failed: %w", err)
	}
	docsDir := filepath.Join(root, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		return fmt.Errorf("prepare project docs dir failed: %w", err)
	}

	readmeContent := fmt.Sprintf(
		"# %s\n\n%s\n\n## 目标\n%s\n",
		appName,
		blueprint.template.Summary,
		blueprint.template.BusinessGoal,
	)
	if err := writeFileIfAbsent(filepath.Join(root, "README.md"), readmeContent); err != nil {
		return err
	}

	if err := writeFileIfAbsent(filepath.Join(docsDir, "PRODUCT_BRIEF.md"), blueprint.designBrief+"\n"); err != nil {
		return err
	}
	return nil
}

func writeFileIfAbsent(path, content string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect file %s failed: %w", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write file %s failed: %w", path, err)
	}
	return nil
}

func renderSolutionRequirementPrompt(appName string, spec solutionRequirementSpec) string {
	builder := strings.Builder{}
	builder.WriteString("应用名称：")
	builder.WriteString(appName)
	builder.WriteString("\n\n目标：")
	builder.WriteString(spec.objective)
	builder.WriteString("\n\n交付物：\n")
	for _, item := range spec.deliverable {
		builder.WriteString("- ")
		builder.WriteString(strings.TrimSpace(item))
		builder.WriteString("\n")
	}
	builder.WriteString("\n验收标准：\n")
	for _, item := range spec.acceptance {
		builder.WriteString("- ")
		builder.WriteString(strings.TrimSpace(item))
		builder.WriteString("\n")
	}
	builder.WriteString("\n执行要求：直接在代码中实现并补齐测试，输出关键变更说明。")
	return strings.TrimSpace(builder.String())
}

func slugifyAppName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" {
		return "real-app"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	result := strings.Trim(b.String(), "-")
	if result == "" {
		return "real-app"
	}
	return result
}

func builtinSolutionBlueprintCatalog() map[string]solutionBlueprint {
	return map[string]solutionBlueprint{
		"saas-ticket-knowledge": {
			template: v1SolutionTemplate{
				ID:               "saas-ticket-knowledge",
				Name:             "智能工单与知识库 SaaS",
				Summary:          "面向客服团队的多租户工单系统，包含知识库、SLA、自动分派和运营看板。",
				BusinessGoal:     "将平均工单处理时长降低 30%，并支持团队规模化协作。",
				SuggestedStack:   "Go + React + SQLite/PostgreSQL + Redis + Playwright",
				RequirementCount: 8,
			},
			designBrief: strings.TrimSpace(`
# 产品设计简报

## 场景
该系统用于企业客服团队集中处理多渠道工单，沉淀知识库并追踪 SLA 兑现率。

## 核心能力
1. 多租户组织与角色权限（管理员、组长、坐席）。
2. 工单全生命周期（创建、分派、处理、升级、关闭）。
3. 知识库与工单联动（推荐答案、引用沉淀）。
4. SLA 告警与运营看板（超时预警、处理效率统计）。

## 非功能指标
- P95 接口响应 < 300ms
- 核心链路具备端到端自动化测试
- 发布可回滚且具备运行监控
`),
			requirements: []solutionRequirementSpec{
				{
					title:     "业务域建模与架构骨架",
					objective: "定义工单、知识库、用户、SLA 的领域模型并搭建可运行骨架。",
					deliverable: []string{
						"后端模块分层与路由骨架",
						"前端页面框架与导航结构",
						"基础数据模型与迁移脚本",
					},
					acceptance: []string{
						"项目可本地启动并通过基础健康检查",
						"关键实体模型字段齐全且命名一致",
					},
				},
				{
					title:     "身份认证与权限控制",
					objective: "实现登录鉴权和多角色访问控制。",
					deliverable: []string{
						"登录、登出、会话保持",
						"角色权限中间件",
						"权限不足时的前后端统一处理",
					},
					acceptance: []string{
						"未登录访问受保护接口返回 401",
						"不同角色操作边界由测试覆盖",
					},
				},
				{
					title:     "工单管理核心流程",
					objective: "实现工单创建、指派、状态流转和备注协同。",
					deliverable: []string{
						"工单列表/详情/筛选",
						"状态流转与责任人变更",
						"工单评论与操作日志",
					},
					acceptance: []string{
						"状态流转合法性有后端校验",
						"前端可完整操作一条工单从创建到关闭",
					},
				},
				{
					title:     "知识库与智能推荐",
					objective: "实现知识条目管理并在工单处理时给出关联建议。",
					deliverable: []string{
						"知识库增删改查",
						"工单文本与知识条目匹配接口",
						"处理页中的推荐展示与一键引用",
					},
					acceptance: []string{
						"知识条目变更可即时生效",
						"工单页可展示推荐并写入处理记录",
					},
				},
				{
					title:     "SLA 与告警机制",
					objective: "构建超时判定、优先级策略和预警通知。",
					deliverable: []string{
						"SLA 规则配置",
						"超时判定任务与状态标记",
						"超时预警（站内或日志告警）",
					},
					acceptance: []string{
						"可配置不同优先级 SLA 阈值",
						"超时工单在列表中可被准确识别",
					},
				},
				{
					title:     "运营看板与指标统计",
					objective: "提供处理效率、积压量、超时率等运营指标。",
					deliverable: []string{
						"聚合统计接口",
						"看板页面与图表展示",
						"时间区间筛选能力",
					},
					acceptance: []string{
						"统计口径文档化并与接口一致",
						"看板主要指标可随筛选条件联动",
					},
				},
				{
					title:     "自动化测试与质量闸门",
					objective: "补齐单测、集成测试与页面冒烟测试，建立质量闸门。",
					deliverable: []string{
						"后端核心服务单元测试",
						"前端关键交互测试",
						"端到端主流程 smoke 用例",
					},
					acceptance: []string{
						"CI 可执行测试并给出明确结果",
						"核心流程回归可被自动检测",
					},
				},
				{
					title:     "部署发布与运行保障",
					objective: "完成可部署配置、运行监控和上线操作文档。",
					deliverable: []string{
						"生产配置模板与启动脚本",
						"日志/指标观测说明",
						"发布与回滚手册",
					},
					acceptance: []string{
						"可在目标环境完成一次端到端部署",
						"发布失败可按文档执行回滚",
					},
				},
			},
		},
		"b2b-order-fulfillment": {
			template: v1SolutionTemplate{
				ID:               "b2b-order-fulfillment",
				Name:             "B2B 订单履约平台",
				Summary:          "面向渠道商的订单履约系统，覆盖下单、库存锁定、发货与对账。",
				BusinessGoal:     "提高订单履约准时率并降低人工对账成本。",
				SuggestedStack:   "Go + React + PostgreSQL + MQ + Playwright",
				RequirementCount: 8,
			},
			designBrief: strings.TrimSpace(`
# 产品设计简报

## 场景
该平台服务于 B2B 渠道分销，目标是把订单流、库存流、物流流和结算流打通。

## 核心能力
1. 渠道商下单与审批。
2. 库存预占与缺货处理。
3. 出库发货与物流跟踪。
4. 对账、差异处理与报表。

## 非功能指标
- 履约主链路全量可追踪
- 日终对账自动化可重跑
- 关键失败场景可回放与补偿
`),
			requirements: []solutionRequirementSpec{
				{
					title:     "订单域与流程建模",
					objective: "建立订单、订单项、状态机与业务校验基础。",
					deliverable: []string{
						"订单创建与查询接口",
						"订单状态机与状态日志",
						"基础页面与查询筛选",
					},
					acceptance: []string{
						"状态流转有严谨校验",
						"订单数据可正确持久化与读取",
					},
				},
				{
					title:     "库存预占与释放",
					objective: "实现库存锁定、释放和扣减策略。",
					deliverable: []string{
						"库存台账模型与接口",
						"下单预占与取消释放",
						"缺货处理策略",
					},
					acceptance: []string{
						"并发下库存不出现负数",
						"订单取消可触发库存回补",
					},
				},
				{
					title:     "履约与发货管理",
					objective: "支持拣货、出库、发货和物流单号追踪。",
					deliverable: []string{
						"发货单模型与状态",
						"物流信息录入与查询",
						"履约异常标记",
					},
					acceptance: []string{
						"履约节点状态可追踪",
						"物流信息可在前端完整展示",
					},
				},
				{
					title:     "对账中心与差异处理",
					objective: "实现订单与发货、结算数据的一致性对账。",
					deliverable: []string{
						"对账任务与结果存储",
						"差异明细页与处理动作",
						"重跑对账能力",
					},
					acceptance: []string{
						"可识别至少三类差异场景",
						"差异处理结果可审计",
					},
				},
				{
					title:     "审批与权限体系",
					objective: "提供下单审批、异常审批与角色权限。",
					deliverable: []string{
						"审批流模型",
						"审批动作接口",
						"角色权限控制",
					},
					acceptance: []string{
						"未审批订单不能进入履约",
						"审批记录完整可追踪",
					},
				},
				{
					title:     "运营报表与导出",
					objective: "输出履约率、延迟率、差异率等关键报表。",
					deliverable: []string{
						"报表聚合接口",
						"统计看板",
						"报表导出能力",
					},
					acceptance: []string{
						"主要指标与原始数据一致",
						"报表支持按时间和渠道筛选",
					},
				},
				{
					title:     "测试体系与异常回放",
					objective: "构建多层测试并支持失败链路回放。",
					deliverable: []string{
						"核心服务单测",
						"跨模块集成测试",
						"异常回放脚本",
					},
					acceptance: []string{
						"关键链路测试覆盖率达标",
						"可重现并排查典型失败场景",
					},
				},
				{
					title:     "上线准备与运维手册",
					objective: "提供部署、监控、告警和回滚保障。",
					deliverable: []string{
						"部署配置与脚本",
						"监控与告警规则",
						"运维与回滚文档",
					},
					acceptance: []string{
						"生产部署流程可演练通过",
						"出现故障可按手册快速恢复",
					},
				},
			},
		},
	}
}
