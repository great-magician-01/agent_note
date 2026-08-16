package services

// OnNoteContentChanged 笔记内容变化钩子：worker 包启动时注入实现（元数据异步生成）。
// 默认空操作，保证未启动 worker 时接口可用。
var OnNoteContentChanged = func(noteID int64) {}
