// @bytemd/vue-next 官方 d.ts 仅导出 DefineComponent 类型，verbatimModuleSyntax 下无法作为组件值使用。
// 这里补全为可实例化的组件声明。
declare module '@bytemd/vue-next' {
  import type { DefineComponent } from 'vue'
  import type { BytemdPlugin } from 'bytemd'

  interface EditorProps {
    value?: string
    plugins?: BytemdPlugin[]
    mode?: 'split' | 'tab' | 'auto'
    placeholder?: string
    uploadImages?: (files: File[]) => Promise<{ url: string; alt?: string; title?: string }[]>
    overridePreview?: (el: HTMLElement, props: unknown) => void
    maxEditorWidth?: number
    sanitize?: (schema: unknown) => unknown
    locale?: unknown
    editorConfig?: unknown
  }

  export const Editor: DefineComponent<EditorProps>
  export const Viewer: DefineComponent<{ value?: string; plugins?: BytemdPlugin[] }>
}
