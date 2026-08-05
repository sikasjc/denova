import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useChat as useAIChat } from '@ai-sdk/react'
import { useTranslation } from 'react-i18next'
import {
  abortChat,
  analyzeChatContext,
  createSession,
  deleteSession,
  executeCommand,
  getActiveChatTask,
  getMessagesPage,
  getSessions,
  renameSession,
  switchSession,
  DEFAULT_SESSION_MESSAGE_PAGE_SIZE,
} from '@/lib/api'
import type { ContextAnalysis, IDEContext, SessionSummary, TextSelection } from '@/lib/api'
import type { WritingIntent } from '@/features/settings/types'
import { APIError } from '@/lib/api-client'
import type { UserMessageReference } from '@/lib/api-client/types'
import { fetchSettings } from '@/features/settings/api'
import { formatApprovedPlanExecutionMessage } from '@/lib/plan-mode'
import {
  AgentChatTransport,
  buildAgentChatRequestBody,
  normalizeAgentUIMessages,
  type AgentUIMessage,
} from '@/lib/agent-ui'
import { agentViewContent, buildAgentMessageViews, isPlanProtocolToolName, type AgentMessageView, type AgentPartRef } from '@/lib/agent-message-view'
import { isWorkspaceChangeForWorkspace, type WorkspaceChangeEvent } from '@/features/changes/types'

interface ChatOptions {
  workspace?: string
  onAgentFileChange?: (path?: string) => void | Promise<void>
  onWorkspaceChange?: (event: WorkspaceChangeEvent) => void | Promise<void>
}

export interface ChatSendOptions {
  writingSkill?: string
  writingIntent?: WritingIntent
  ideContext?: IDEContext
  imagePresetId?: string
  tellerId?: string
  planMode?: boolean
  displayMessage?: string
  hideUserMessage?: boolean
  reviewFeedback?: Array<{
    source?: 'workspace_change' | 'document'
    reviewThreadId: string
    commentIds: string[]
  }>
  reviewFeedbackDisplay?: {
    comments: Array<{ id: string; body: string; path?: string; review_path?: string; review_line?: number }>
  }
  loreReferenceLabels?: Record<string, string>
  onSubmissionStart?: () => void
  onSubmissionError?: (error: unknown) => void
}

export function useAgentChat(options: ChatOptions = {}) {
  const { t } = useTranslation()
  const { workspace = '', onAgentFileChange, onWorkspaceChange } = options
  const transport = useMemo(() => new AgentChatTransport(), [])
  const {
    messages: uiMessages,
    setMessages: setUIMessages,
    sendMessage,
    resumeStream,
    stop: stopAIStream,
    status,
  } = useAIChat<AgentUIMessage>({
    transport,
    throttle: 60,
    onData: (part) => {
      if (part.type !== 'data-agent-workspace-change') return
      const event = part.data as WorkspaceChangeEvent
      if (!isWorkspaceChangeForWorkspace(event, workspace)) return
      window.dispatchEvent(new CustomEvent('nova:workspace-change', { detail: event }))
      void onWorkspaceChange?.(event)
    },
    onFinish: () => {
      void onAgentFileChange?.()
      // 一轮结束后（非流式期）按常驻上限裁剪，AI SDK 此刻不再写入该 state，裁剪安全。
      trimResidentMessagesRef.current()
    },
  })
  const messages = useMemo(() => normalizeAgentUIMessages(uiMessages), [uiMessages])
  // 镜像最新 uiMessages，供 onFinish（渲染外）读取当前长度做常驻裁剪判断。
  const uiMessagesRef = useRef<AgentUIMessage[]>(uiMessages)
  uiMessagesRef.current = uiMessages
  const isStreaming = status === 'submitted' || status === 'streaming'
  const activityContent = status === 'submitted' ? t('chat.activity.thinking') : ''
  const [sessions, setSessions] = useState<SessionSummary[]>([])
  const [activeSessionId, setActiveSessionId] = useState('')
  const [references, setReferences] = useState<string[]>([])
  const [loreReferences, setLoreReferences] = useState<string[]>([])
  const [styleScenes, setStyleScenes] = useState<string[]>([])
  const [textSelections, setTextSelections] = useState<TextSelection[]>([])
  const [defaultPlanMode, setDefaultPlanMode] = useState(false)
  const [planModes, setPlanModes] = useState<Record<string, boolean>>(() => readChatPlanModes())
  const [hasEarlierMessages, setHasEarlierMessages] = useState(false)
  const [isLoadingEarlierHistory, setIsLoadingEarlierHistory] = useState(false)
  const historyRequestGenerationRef = useRef(0)
  const earlierHistoryRequestRef = useRef(0)
  const earlierHistoryLoadingRef = useRef(false)
  const historyPageRef = useRef<{ sessionId?: string; nextBefore: string; hasMore: boolean }>({
    nextBefore: '0',
    hasMore: false,
  })
  // 前端常驻消息上限（0/未设置=不限制）。用于超长多轮会话的内存控制。
  const residentMessageLimitRef = useRef(0)
  // 用户显式"加载更早"后暂停自动裁剪，尊重其查看历史的意图；切换/重载会话时重置。
  const manualHistoryExpandedRef = useRef(false)
  // 记录常驻窗口是否因裁剪而截断了更早消息（用于展示"加载更早"入口）。
  const residentTrimmedRef = useRef(false)
  const trimResidentMessagesRef = useRef<() => void>(() => {})
  const activePlanMode = planModeForSession(planModes, activeSessionId, defaultPlanMode)

  useEffect(() => {
    let cancelled = false
    fetchSettings()
      .then((data) => {
        if (cancelled) return
        setDefaultPlanMode(data.effective?.plan_mode_default === true)
        const limit = data.effective?.chat_resident_message_limit
        residentMessageLimitRef.current = typeof limit === 'number' && limit > 0 ? limit : 0
      })
      .catch((e) => console.warn('加载 Agent 聊天配置失败', e))
    return () => { cancelled = true }
  }, [])

  const setSessionPlanMode = useCallback((sessionId: string, value: boolean) => {
    const id = sessionId || 'default'
    setPlanModes((current) => {
      const next = { ...current, [id]: value }
      writeChatPlanModes(next)
      return next
    })
  }, [])

  const setActivePlanMode = useCallback((value: boolean) => {
    setSessionPlanMode(activeSessionId || 'default', value)
  }, [activeSessionId, setSessionPlanMode])

  const togglePlanMode = useCallback(() => {
    setActivePlanMode(!activePlanMode)
  }, [activePlanMode, setActivePlanMode])

  const loadSessions = useCallback(async () => {
    try {
      const list = await getSessions()
      setSessions(list)
      setActiveSessionId(list.find(item => item.active)?.id || list[0]?.id || '')
      return list
    } catch (e) {
      console.error('加载会话列表失败', e)
      return []
    }
  }, [])

  const loadHistory = useCallback(async (sessionId?: string) => {
    const generation = historyRequestGenerationRef.current + 1
    historyRequestGenerationRef.current = generation
    earlierHistoryRequestRef.current += 1
    earlierHistoryLoadingRef.current = false
    manualHistoryExpandedRef.current = false
    residentTrimmedRef.current = false
    setIsLoadingEarlierHistory(false)
    try {
      const page = await getMessagesPage(sessionId)
      if (generation !== historyRequestGenerationRef.current) return
      historyPageRef.current = {
        sessionId,
        nextBefore: page.nextBefore,
        hasMore: page.hasMore,
      }
      setHasEarlierMessages(page.hasMore)
      setUIMessages(filterInternalPlanUIMessages(page.messages))
    } catch (e) {
      if (generation === historyRequestGenerationRef.current) console.error('加载历史失败', e)
    }
  }, [setUIMessages])

  const loadEarlierHistory = useCallback(async () => {
    // 用户显式查看更早历史：暂停后续自动裁剪，避免刚拉回又被裁掉。
    manualHistoryExpandedRef.current = true
    const currentPage = historyPageRef.current
    if (earlierHistoryLoadingRef.current) return
    // 常驻裁剪过的会话，"更早消息"来自被裁掉的部分而非分页游标之前；
    // 直接以更大的窗口从后端重新拉取一段连续历史，游标准确、无空洞。
    const trimmed = residentTrimmedRef.current
    if (!trimmed && !currentPage.hasMore) return
    const historyGeneration = historyRequestGenerationRef.current
    const requestID = earlierHistoryRequestRef.current + 1
    earlierHistoryRequestRef.current = requestID
    earlierHistoryLoadingRef.current = true
    setIsLoadingEarlierHistory(true)
    try {
      if (trimmed) {
        const window = residentMessageLimitRef.current + DEFAULT_SESSION_MESSAGE_PAGE_SIZE
        const page = await getMessagesPage(currentPage.sessionId, { limit: window })
        if (historyGeneration !== historyRequestGenerationRef.current || requestID !== earlierHistoryRequestRef.current) return
        residentTrimmedRef.current = false
        historyPageRef.current = {
          sessionId: currentPage.sessionId,
          nextBefore: page.nextBefore,
          hasMore: page.hasMore,
        }
        setHasEarlierMessages(page.hasMore)
        setUIMessages(filterInternalPlanUIMessages(page.messages))
        return
      }
      const page = await getMessagesPage(currentPage.sessionId, { before: currentPage.nextBefore })
      if (historyGeneration !== historyRequestGenerationRef.current || requestID !== earlierHistoryRequestRef.current) return
      const earlierMessages = filterInternalPlanUIMessages(page.messages)
      historyPageRef.current = {
        ...currentPage,
        nextBefore: page.nextBefore,
        hasMore: page.hasMore,
      }
      setHasEarlierMessages(page.hasMore)
      setUIMessages((messages) => normalizeAgentUIMessages([...earlierMessages, ...messages]))
    } catch (e) {
      if (historyGeneration === historyRequestGenerationRef.current && requestID === earlierHistoryRequestRef.current) {
        console.error('加载更早历史失败', e)
      }
    } finally {
      if (requestID === earlierHistoryRequestRef.current) {
        earlierHistoryLoadingRef.current = false
        setIsLoadingEarlierHistory(false)
      }
    }
  }, [setUIMessages])

  useEffect(() => () => {
    historyRequestGenerationRef.current += 1
    earlierHistoryRequestRef.current += 1
  }, [])

  // 常驻裁剪：一轮结束后把 React state 中的消息裁到最近 N 条，控制超长会话的内存占用。
  // 被裁掉的更早消息不会丢失——它们已持久化在后端，可通过"加载更早"重新拉回。
  const trimResidentMessages = useCallback(() => {
    const limit = residentMessageLimitRef.current
    if (limit <= 0 || manualHistoryExpandedRef.current) return
    if (uiMessagesRef.current.length <= limit) return
    residentTrimmedRef.current = true
    setUIMessages((current) => (current.length <= limit ? current : current.slice(current.length - limit)))
    setHasEarlierMessages(true)
  }, [setUIMessages])
  trimResidentMessagesRef.current = trimResidentMessages

  const addReference = useCallback((path: string) => {
    setReferences(prev => Array.from(new Set([...prev, path])))
  }, [])
  const addLoreReference = useCallback((id: string) => {
    setLoreReferences(prev => Array.from(new Set([...prev, id])))
  }, [])
  const removeReference = useCallback((path: string) => {
    setReferences(prev => prev.filter(item => item !== path))
  }, [])
  const removeLoreReference = useCallback((id: string) => {
    setLoreReferences(prev => prev.filter(item => item !== id))
  }, [])
  const addStyleScene = useCallback((scene: string) => {
    setStyleScenes(prev => Array.from(new Set([...prev, scene])))
  }, [])
  const removeStyleScene = useCallback((scene: string) => {
    setStyleScenes(prev => prev.filter(item => item !== scene))
  }, [])
  const clearReferences = useCallback(() => setReferences([]), [])
  const clearStyleScenes = useCallback(() => setStyleScenes([]), [])
  const addTextSelection = useCallback((sel: TextSelection) => {
    setTextSelections(prev => [...prev, sel])
  }, [])
  const removeTextSelection = useCallback((index: number) => {
    setTextSelections(prev => prev.filter((_, i) => i !== index))
  }, [])

  const prepareAgentRequest = useCallback((input: string, forcedPlanMode?: boolean) => {
    if (input.startsWith('/')) {
      const cmd = input.slice(1).split(' ')[0]
      if (['clear', 'compact', 'status', 'help'].includes(cmd)) {
        throw new Error(t('chat.contextAnalysis.commandUnavailable'))
      }
    }

    let planMode = forcedPlanMode ?? activePlanMode
    let userMessage = input
    if (input.startsWith('/plan')) {
      planMode = true
      userMessage = input.replace(/^\/plan\s*/, '').trim()
      if (!userMessage) throw new Error(t('chat.planUsage'))
    }

    const inlineReferences = parseInlineReferences(userMessage)
    const inlineStyleScenes = parseInlineStyleScenes(userMessage)
    return {
      message: userMessage,
      references: Array.from(new Set([...references, ...inlineReferences])),
      loreReferences: Array.from(new Set(loreReferences)),
      styleScenes: Array.from(new Set([...styleScenes, ...inlineStyleScenes])),
      textSelections,
      composerReferences: references,
      composerLoreReferences: loreReferences,
      composerStyleScenes: styleScenes,
      composerTextSelections: textSelections,
      planMode,
    }
  }, [activePlanMode, loreReferences, references, styleScenes, t, textSelections])

  const send = useCallback(async (input: string, sendOptions: ChatSendOptions = {}) => {
    if (isStreaming) return false
    const command = agentBypassCommand(input)
    if (command) {
      const result = await executeCommand(command)
      if (command === 'clear') {
        await loadHistory()
        await loadSessions()
        return true
      }
      appendDataMessage(setUIMessages, 'data-agent-system', { content: result })
      return true
    }

    let prepared: ReturnType<typeof prepareAgentRequest>
    try {
      prepared = prepareAgentRequest(input, sendOptions.planMode)
    } catch (e) {
      appendDataMessage(setUIMessages, 'data-agent-system', { content: (e as Error).message })
      return false
    }
    if (prepared.planMode !== activePlanMode || sendOptions.planMode !== undefined) {
      setActivePlanMode(prepared.planMode)
    }

    const body = buildAgentChatRequestBody({
      message: prepared.message,
      references: prepared.references,
      lore_references: prepared.loreReferences,
      style_scenes: prepared.styleScenes,
      selections: prepared.textSelections.map(s => ({
        file_name: s.fileName,
        start_line: s.startLine,
        end_line: s.endLine,
        content: s.content,
      })),
      ide_context: normalizeIDEContext(sendOptions.ideContext),
      plan_mode: prepared.planMode,
      writing_skill: sendOptions.writingSkill,
      writing_intent: sendOptions.writingIntent,
      image_preset_id: sendOptions.imagePresetId,
      teller_id: sendOptions.tellerId,
      review_feedback: sendOptions.reviewFeedback?.map((feedback) => ({
        source: feedback.source,
        review_thread_id: feedback.reviewThreadId,
        comment_ids: feedback.commentIds,
      })),
    } as Parameters<typeof buildAgentChatRequestBody>[0] & { message: string }) as Record<string, unknown>
    body.message = prepared.message

    const userReferences = buildUserMessageReferences(prepared, sendOptions)
    let submissionStarted = false
    try {
      const pendingRequest = sendMessage({
        role: 'user',
        metadata: {
          ...(sendOptions.hideUserMessage ? { display_hidden: true } : {}),
          ...(userReferences.length ? { user_references: userReferences } : {}),
        },
        parts: [{ type: 'text', text: sendOptions.displayMessage || input }],
      }, { body })
      setReferences((current) => current.filter((item) => !prepared.composerReferences.includes(item)))
      setLoreReferences((current) => current.filter((item) => !prepared.composerLoreReferences.includes(item)))
      setStyleScenes((current) => current.filter((item) => !prepared.composerStyleScenes.includes(item)))
      setTextSelections((current) => current.filter((item) => !prepared.composerTextSelections.includes(item)))
      submissionStarted = true
      sendOptions.onSubmissionStart?.()
      await pendingRequest
      return true
    } catch (e) {
      if (submissionStarted) {
        setReferences((current) => Array.from(new Set([...prepared.composerReferences, ...current])))
        setLoreReferences((current) => Array.from(new Set([...prepared.composerLoreReferences, ...current])))
        setStyleScenes((current) => Array.from(new Set([...prepared.composerStyleScenes, ...current])))
        setTextSelections((current) => [...prepared.composerTextSelections.filter((item) => !current.includes(item)), ...current])
        sendOptions.onSubmissionError?.(e)
      }
      appendDataMessage(setUIMessages, 'data-agent-error', { content: t('chat.activity.requestFailed', { error: agentRequestErrorMessage(t, e) }) })
      return false
    }
  }, [activePlanMode, isStreaming, loadHistory, loadSessions, prepareAgentRequest, sendMessage, setActivePlanMode, setUIMessages, t])

  const analyzeContext = useCallback(async (input: string, sendOptions: ChatSendOptions = {}): Promise<ContextAnalysis> => {
    if (isStreaming) throw new Error(t('chat.contextAnalysis.streamingUnavailable'))
    const prepared = prepareAgentRequest(input)
    return analyzeChatContext(prepared.message, prepared.references, prepared.loreReferences, prepared.styleScenes, prepared.textSelections, prepared.planMode, sendOptions.writingSkill, sendOptions.ideContext, sendOptions.imagePresetId, sendOptions.tellerId, sendOptions.writingIntent)
  }, [isStreaming, prepareAgentRequest, t])

  const submitPlanQuestion = useCallback((ref: AgentPartRef, content: string, _preview: string) => {
    setUIMessages(prev => markPlanUIMessageAction(prev, ref, 'answered'))
    void send(content, { planMode: true, hideUserMessage: true })
  }, [send, setUIMessages])

  const approveProposedPlan = useCallback((ref: AgentPartRef) => {
    const planView = findAgentMessageView(messages, ref)
    const plan = planView ? agentViewContent(planView) : ''
    if (!plan.trim()) return
    const userContext = collectPlanUserContext(messages, ref)
    setUIMessages(prev => markPlanUIMessageAction(prev, ref, 'approved'))
    void send(formatApprovedPlanExecutionMessage(plan, userContext), {
      planMode: false,
      hideUserMessage: true,
    })
  }, [messages, send, setUIMessages])

  const exitPlanMode = useCallback(() => {
    setActivePlanMode(false)
  }, [setActivePlanMode])

  const resumeActiveChat = useCallback(async () => {
    if (isStreaming) return
    try {
      const activeTask = await getActiveChatTask()
      if (!activeTask.active) return
      await resumeStream()
    } catch (e) {
      if (!isAbortError(e)) console.error('恢复聊天流失败', e)
    }
  }, [isStreaming, resumeStream])

  const stop = useCallback(() => {
    void abortChat()
    stopAIStream()
  }, [stopAIStream])

  const createChatSession = useCallback(async (title?: string) => {
    const session = await createSession(title)
    setActiveSessionId(session.id)
    await Promise.all([loadSessions(), loadHistory(session.id)])
    await resumeActiveChat()
  }, [loadHistory, loadSessions, resumeActiveChat])

  const switchChatSession = useCallback(async (id: string) => {
    if (!id || id === activeSessionId) return
    const previousSessionId = activeSessionId
    if (isStreaming) stopAIStream()
    setActiveSessionId(id)

    let session: SessionSummary
    try {
      session = await switchSession(id)
    } catch (error) {
      setActiveSessionId((current) => current === id ? previousSessionId : current)
      throw error
    }

    setActiveSessionId(session.id)
    await Promise.all([loadSessions(), loadHistory(session.id)])
    await resumeActiveChat()
  }, [activeSessionId, isStreaming, loadHistory, loadSessions, resumeActiveChat, stopAIStream])

  const renameChatSession = useCallback(async (id: string, title: string) => {
    await renameSession(id, title)
    await loadSessions()
  }, [loadSessions])

  const deleteChatSession = useCallback(async (id: string) => {
    stopAIStream()
    const session = await deleteSession(id)
    setActiveSessionId(session.id)
    await Promise.all([loadSessions(), loadHistory(session.id)])
    await resumeActiveChat()
  }, [loadHistory, loadSessions, resumeActiveChat, stopAIStream])

  return {
    messages,
    sessions,
    activeSessionId,
    isStreaming,
    activityContent,
    references,
    loreReferences,
    styleScenes,
    textSelections,
    planMode: activePlanMode,
    setPlanMode: setActivePlanMode,
    togglePlanMode,
    send,
    analyzeContext,
    submitPlanQuestion,
    approveProposedPlan,
    exitPlanMode,
    stop,
    loadSessions,
    loadHistory,
    loadEarlierHistory,
    hasEarlierMessages,
    isLoadingEarlierHistory,
    resumeActiveChat,
    createChatSession,
    switchChatSession,
    renameChatSession,
    deleteChatSession,
    addReference,
    removeReference,
    addLoreReference,
    removeLoreReference,
    addStyleScene,
    removeStyleScene,
    addTextSelection,
    removeTextSelection,
    clearReferences,
    clearStyleScenes,
  }
}

function buildUserMessageReferences(
  prepared: {
    references: string[]
    loreReferences: string[]
    styleScenes: string[]
    textSelections: TextSelection[]
  },
  options: ChatSendOptions,
): UserMessageReference[] {
  const result: UserMessageReference[] = []
  for (const path of prepared.references) result.push({ kind: 'file', label: path })
  for (const id of prepared.loreReferences) result.push({ kind: 'lore', id, label: options.loreReferenceLabels?.[id] || id })
  for (const scene of prepared.styleScenes) result.push({ kind: 'style', label: scene })
  for (const selection of prepared.textSelections) {
    result.push({
      kind: 'selection',
      label: selection.fileName,
      start_line: selection.startLine,
      end_line: selection.endLine,
      detail: boundedReferenceDetail(selection.content),
    })
  }
  for (const comment of options.reviewFeedbackDisplay?.comments ?? []) {
    result.push({
      kind: 'review_comment',
      id: comment.id,
      label: comment.review_path || comment.path || comment.id,
      ...(comment.review_line !== undefined ? { start_line: comment.review_line, end_line: comment.review_line } : {}),
      detail: boundedReferenceDetail(comment.body),
    })
  }
  return result
}

function boundedReferenceDetail(value: string): string {
  const normalized = value.trim()
  return normalized.length > 512 ? `${normalized.slice(0, 512)}…` : normalized
}

function normalizeIDEContext(context?: IDEContext) {
  if (!context?.currentFile && !context?.openFiles?.length) return undefined
  return {
    current_file: context.currentFile || undefined,
    open_files: context.openFiles?.length ? context.openFiles : undefined,
  }
}

function appendDataMessage(setUIMessages: (updater: (messages: AgentUIMessage[]) => AgentUIMessage[]) => void, type: `data-agent-${string}`, data: Record<string, unknown>) {
  setUIMessages(messages => [
    ...messages,
    {
      id: `${type}-${Date.now()}-${messages.length}`,
      role: 'assistant',
      parts: [{ type, data, id: `${type}-${Date.now()}` } as AgentUIMessage['parts'][number]],
    } as AgentUIMessage,
  ])
}

function agentBypassCommand(input: string): string | null {
  if (!input.startsWith('/')) return null
  const cmd = input.slice(1).split(' ')[0]
  return ['clear', 'compact', 'status', 'help'].includes(cmd) ? cmd : null
}

function parseInlineReferences(input: string): string[] {
  const result = new Set<string>()
  const regex = /(?:^|\s)@([^\s@]+)/g
  let match: RegExpExecArray | null
  while ((match = regex.exec(input)) !== null) {
    const value = match[1]
    if (value.startsWith('资料:')) continue
    result.add(value)
  }
  return Array.from(result)
}

function parseInlineStyleScenes(input: string): string[] {
  const result = new Set<string>()
  const regex = /(?:^|\s)#([^\s#]+)/g
  let match: RegExpExecArray | null
  while ((match = regex.exec(input)) !== null) result.add(match[1])
  return Array.from(result)
}

const CHAT_PLAN_MODES_STORAGE_KEY = 'nova.chat.plan_modes.v1'

function readChatPlanModes(): Record<string, boolean> {
  if (typeof window === 'undefined') return {}
  const raw = window.localStorage.getItem(CHAT_PLAN_MODES_STORAGE_KEY)
  if (!raw) return {}
  try {
    const parsed = JSON.parse(raw)
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return {}
    const result: Record<string, boolean> = {}
    for (const [key, value] of Object.entries(parsed)) {
      if (typeof key === 'string' && typeof value === 'boolean') result[key] = value
    }
    return result
  } catch {
    return {}
  }
}

function writeChatPlanModes(value: Record<string, boolean>) {
  if (typeof window === 'undefined') return
  window.localStorage.setItem(CHAT_PLAN_MODES_STORAGE_KEY, JSON.stringify(value))
}

function planModeForSession(planModes: Record<string, boolean>, sessionId: string, defaultValue: boolean) {
  const id = sessionId || 'default'
  return planModes[id] ?? defaultValue
}

function findAgentMessageView(messages: AgentUIMessage[], ref: AgentPartRef): AgentMessageView | undefined {
  return buildAgentMessageViews(messages).find((view) => sameAgentPartRef(view.ref, ref))
}

function collectPlanUserContext(messages: AgentUIMessage[], target: AgentPartRef) {
  const views = buildAgentMessageViews(messages)
  const planIndex = views.findIndex((view) => sameAgentPartRef(view.ref, target))
  const end = planIndex >= 0 ? planIndex : views.length
  let start = 0
  for (let i = end - 1; i >= 0; i -= 1) {
    if (views[i].kind === 'proposed-plan') {
      start = i + 1
      break
    }
  }
  const userMessages = views
    .slice(start, end)
    .filter((view) => view.kind === 'user')
    .map((view) => agentViewContent(view).trim())
    .filter(Boolean)
  if (userMessages.length <= 1) return userMessages[0] || ''
  return [
    `原始请求：\n${userMessages[0]}`,
    `用户补充：\n${userMessages.slice(1).join('\n\n')}`,
  ].join('\n\n')
}

function filterInternalPlanUIMessages(messages: AgentUIMessage[]) {
  return messages.filter((message) => {
    const text = message.parts.map(part => part.type === 'text' ? part.text : '').join('')
    if (message.role === 'user' && isPlanQuestionAnswerProtocol(text)) return false
    return !message.parts.some(part => isPlanProtocolToolPart(part))
  })
}

function isPlanQuestionAnswerProtocol(content: string) {
  return content.includes('<plan_question_answers>') || content.includes('</plan_question_answers>')
}

function isPlanProtocolToolPart(part: AgentUIMessage['parts'][number]) {
  if (part.type === 'dynamic-tool') return isPlanProtocolToolName(part.toolName)
  if (part.type.startsWith('tool-')) return isPlanProtocolToolName(part.type.replace(/^tool-/, ''))
  return false
}

function markPlanUIMessageAction(
  messages: AgentUIMessage[],
  target: AgentPartRef,
  action: AgentPlanAction,
) {
  return messages.map(message => ({
    ...message,
    parts: message.parts.map((part, index) => {
      const raw = part as Record<string, unknown>
      const type = typeof raw.type === 'string' ? raw.type : ''
      if (!type.startsWith('data-agent-plan-')) return part
      const data = 'data' in part && part.data && typeof part.data === 'object' && !Array.isArray(part.data)
        ? part.data as Record<string, unknown>
        : {}
      const partID = 'id' in part && typeof part.id === 'string' ? part.id : `${message.id}:${index}`
      const candidate = { messageId: message.id, partId: partID, partIndex: index, type }
      if (!sameAgentPartRef(candidate, target)) return part
      return { ...part, data: { ...data, plan_action: action, status: 'success' } } as AgentUIMessage['parts'][number]
    }),
  }))
}

type AgentPlanAction = 'answered' | 'approved' | 'continue' | 'exited'

function sameAgentPartRef(left: AgentPartRef, right: AgentPartRef) {
  return left.messageId === right.messageId
    && left.partIndex === right.partIndex
    && left.partId === right.partId
    && left.type === right.type
}

function isAbortError(error: unknown) {
  return error instanceof DOMException && error.name === 'AbortError'
}

function agentRequestErrorMessage(t: ReturnType<typeof useTranslation>['t'], error: unknown) {
  if (error instanceof APIError) {
    if (error.code === 'review_feedback_outdated') return t('changes.feedback.outdated')
    if (error.code) return t(`changes.error.${error.code}`, { defaultValue: error.message })
    return error.message
  }
  return error instanceof Error ? error.message : String(error)
}
