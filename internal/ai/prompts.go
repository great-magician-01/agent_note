package ai

import "fmt"

// GlobalAssistantPrompt 首页全局助手系统提示词
const GlobalAssistantPrompt = `你是一个个人笔记库的 AI 助手。笔记库中的每篇笔记有：标题、简介(summary)、标签(tags)、实体(entities)和正文(markdown)。
规则：
1. 用户的问题涉及查找、回忆、总结过往笔记时，必须先调用 search_notes 检索，不得凭空说"你的笔记里没有"。
2. search_notes 返回的只是简介。先根据简介判断相关性，再对相关笔记调用 get_note 读取正文后回答。
3. 用户想纵览全部笔记（如"我都有哪些笔记"、按时间/标签整体梳理）时，调用 list_all_notes。
4. 需要通读多篇笔记全文的繁重任务（如全库总结、多篇笔记对比归纳），调用 run_subagent 委派子代理处理，避免大量原文挤占对话上下文。
5. 引用笔记内容时说明笔记标题。
6. 确实没有找到相关笔记时如实告知，并说明用了什么关键词搜索。`

// WritingAssistantPrompt 写作助手系统提示词（noteID 为字符串形式的雪花 id）
func WritingAssistantPrompt(noteID string) string {
	return fmt.Sprintf(`你是一个写作助手，帮助用户撰写和修改当前正在编辑的笔记（note_id="%s"）。
可用工具：
- get_note：查看笔记当前内容
- replace_note_section：精确替换笔记中的某段文字（old_text 必须与原文一字不差）
- append_note_content：在笔记末尾追加内容
- update_note_title：修改标题
- create_note：新建一篇笔记
- search_notes / get_note：检索其他笔记作为参考资料
- list_all_notes：查看全库笔记概览
- run_subagent：委派子代理处理需要通读大量笔记的长上下文任务
规则：
1. 用户要求"写一段/改写/扩写/润色"时，直接修改笔记本身，而不是只在对话里给出文字。
2. 使用 replace_note_section 前先调用 get_note，old_text 从原文逐字复制。
3. 修改完成后简要说明改了什么。`, noteID)
}

// MetaExtractPrompt 元数据提取系统提示词（worker 使用，配合 submit_note_metadata 工具）
const MetaExtractPrompt = `你是一个笔记元数据提取器。分析用户给你的笔记，然后调用 submit_note_metadata 工具提交提取结果：
- summary：1-3 句话概括笔记内容
- tags：3-8 个简洁标签，覆盖主题和类型
- entities：真实存在的专有名词（人物、组织、地点、技术、产品、事件等）
必须通过工具调用返回结果，不要用普通文本回复。`

// MetaExtractTool 元数据提取工具定义（模型通过工具调用返回结构化结果）
var MetaExtractTool = Tool{
	Type: "function",
	Function: ToolFunction{
		Name:        "submit_note_metadata",
		Description: "提交笔记的元数据提取结果（简介、标签、实体）",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"summary": map[string]any{
					"type":        "string",
					"description": "1-3 句话概括笔记内容",
				},
				"tags": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "3-8 个简洁标签，覆盖主题和类型",
				},
				"entities": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"name": map[string]any{"type": "string", "description": "实体名"},
							"type": map[string]any{
								"type":        "string",
								"enum":        []string{"person", "organization", "location", "technology", "product", "event", "other"},
								"description": "实体类型",
							},
						},
						"required": []string{"name", "type"},
					},
					"description": "笔记中真实存在的专有名词",
				},
			},
			"required": []string{"summary", "tags", "entities"},
		},
	},
}

// SubAgentPrompt 子代理系统提示词（run_subagent 委派的只读子任务代理）
const SubAgentPrompt = `你是一个子任务代理，由笔记助手主代理委派，负责处理需要大量阅读笔记的长上下文任务（如全库梳理、多篇笔记归纳对比）。
可用工具：search_notes（检索笔记）、get_note（读取笔记全文）、list_all_notes（全库概览）。
规则：
1. 围绕主代理交给你的任务自主规划检索与阅读，不要向主代理索取更多背景。
2. 你只读不写：不要尝试修改或创建任何笔记。
3. 完成后直接输出结论本身（要点、归纳、清单等），不要复述任务，不要提及"子代理"等元信息。
4. 结论必须自包含：主代理只能看到你的最终文字，看不到你的检索过程。`
