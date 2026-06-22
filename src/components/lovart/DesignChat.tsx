'use client';

import React, { useState, useEffect, useRef } from 'react';
import { Paperclip, AtSign, MapPin, Zap, Globe, Loader2, ArrowUp } from 'lucide-react';
import { chatStream, type ChatStreamEvent } from '@/lib/api';

interface Message {
    id: string;
    role: 'user' | 'assistant';
    content: string;
    timestamp: Date;
    // URLs of images produced by tool calls during this turn.
    images?: string[];
    // Transient status line shown while tools run (e.g. "正在生成图片…").
    toolStatus?: string;
    examples?: Array<{
        title: string;
        description: string;
        image: string;
    }>;
}

interface DesignChatProps {
    projectId: string;
    initialPrompt?: string;
    // Called with the canvas element id(s) the backend persisted for
    // tool-generated images, so the host page can place them on the canvas.
    onCanvasElementsCreated?: (ids: string[]) => void;
}

// extractImages pulls image URLs + persisted canvas element ids out of a
// `tool.result` payload (`{ images: [{ url, canvas_element_id }] }`).
function extractImages(result: unknown): { urls: string[]; elementIds: string[] } {
    const urls: string[] = [];
    const elementIds: string[] = [];
    if (result && typeof result === 'object' && 'images' in result) {
        const images = (result as { images?: unknown }).images;
        if (Array.isArray(images)) {
            for (const img of images) {
                if (img && typeof img === 'object') {
                    const url = (img as { url?: unknown }).url;
                    const cid = (img as { canvas_element_id?: unknown }).canvas_element_id;
                    if (typeof url === 'string') urls.push(url);
                    if (typeof cid === 'string') elementIds.push(cid);
                }
            }
        }
    }
    return { urls, elementIds };
}

const exampleProjects = [
    {
        title: 'Wine List',
        description: 'Mimic this effect to generate a poster of ...',
        image: '🍷',
    },
    {
        title: 'Coffee Shop Branding',
        description: 'you are a brand design expert, generate ...',
        image: '☕',
    },
    {
        title: 'Story Board',
        description: 'I NEED A STORY BOARD FOR THIS...',
        image: '📱',
    },
];

export function DesignChat({ projectId, initialPrompt, onCanvasElementsCreated }: DesignChatProps) {
    const [messages, setMessages] = useState<Message[]>([]);
    const [input, setInput] = useState('');
    const [isLoading, setIsLoading] = useState(false);
    const messagesEndRef = useRef<HTMLDivElement>(null);
    // The backend session store owns history; we persist the returned
    // session_id and send it on follow-up turns so context is remembered.
    const sessionIdRef = useRef<string | null>(null);

    const scrollToBottom = () => {
        messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
    };

    useEffect(() => {
        scrollToBottom();
    }, [messages]);

    // Load initial message if prompt is provided
    useEffect(() => {
        if (initialPrompt && messages.length === 0) {
            handleSendMessage(initialPrompt);
        }
    }, [initialPrompt]);

    const handleSendMessage = async (messageText?: string) => {
        const text = messageText || input.trim();
        if (!text || isLoading) return;

        const userMessage: Message = {
            id: Date.now().toString(),
            role: 'user',
            content: text,
            timestamp: new Date(),
        };

        const assistantId = (Date.now() + 1).toString();
        const assistantMessage: Message = {
            id: assistantId,
            role: 'assistant',
            content: '',
            timestamp: new Date(),
        };

        setMessages(prev => [...prev, userMessage, assistantMessage]);
        setInput('');
        setIsLoading(true);

        const patchAssistant = (patch: Partial<Message>) => {
            setMessages(prev =>
                prev.map(m => (m.id === assistantId ? { ...m, ...patch } : m)),
            );
        };
        const appendImages = (urls: string[]) => {
            if (urls.length === 0) return;
            setMessages(prev =>
                prev.map(m =>
                    m.id === assistantId
                        ? { ...m, images: [...(m.images ?? []), ...urls] }
                        : m,
                ),
            );
        };

        const createdElementIds: string[] = [];

        try {
            await chatStream(
                {
                    messages: [{ role: 'user', content: text }],
                    sessionId: sessionIdRef.current,
                    projectId,
                },
                (ev: ChatStreamEvent) => {
                    switch (ev.object) {
                        case 'session':
                            sessionIdRef.current = ev.session_id;
                            break;
                        case 'chat.completion.chunk': {
                            const delta = ev.choices?.[0]?.delta?.content ?? '';
                            if (delta) {
                                setMessages(prev =>
                                    prev.map(m =>
                                        m.id === assistantId
                                            ? { ...m, content: m.content + delta }
                                            : m,
                                    ),
                                );
                            }
                            break;
                        }
                        case 'tool.call':
                            patchAssistant({
                                toolStatus:
                                    ev.name === 'web_search'
                                        ? '正在联网搜索…'
                                        : ev.name === 'edit_image'
                                          ? '正在编辑图片…'
                                          : ev.name === 'generate_image'
                                            ? '正在生成图片…'
                                            : `正在调用 ${ev.name}…`,
                            });
                            break;
                        case 'tool.result': {
                            patchAssistant({ toolStatus: undefined });
                            if (ev.error) break;
                            const { urls, elementIds } = extractImages(ev.result);
                            appendImages(urls);
                            createdElementIds.push(...elementIds);
                            break;
                        }
                        case 'error':
                            patchAssistant({
                                content:
                                    '抱歉，我遇到了一些问题。请稍后再试。',
                                toolStatus: undefined,
                            });
                            break;
                    }
                },
            );

            // Hand persisted canvas element ids to the host page.
            if (createdElementIds.length > 0) {
                onCanvasElementsCreated?.(createdElementIds);
            }
        } catch (error) {
            console.error('Failed to send message:', error);
            patchAssistant({
                content: '抱歉，我遇到了一些问题。请稍后再试。',
                toolStatus: undefined,
            });
        } finally {
            setIsLoading(false);
        }
    };

    const formatDate = (date: Date) => {
        return date.toLocaleDateString('zh-CN', { month: 'short', day: 'numeric', year: 'numeric' });
    };

    return (
        <div className="flex flex-col h-full bg-white rounded-3xl shadow-lg border border-gray-100">
            {/* Chat Messages */}
            <div className="flex-1 overflow-y-auto p-6 space-y-6">
                {messages.length === 0 && (
                    <div className="flex flex-col space-y-6">
                        {/* Welcome Message */}
                        <div className="flex items-start gap-3">
                            <div className="w-8 h-8 bg-black rounded-full flex items-center justify-center flex-shrink-0">
                                <span className="text-white text-sm font-bold">L</span>
                            </div>
                            <div className="flex-1 space-y-2">
                                <h3 className="text-base font-semibold text-gray-900">Hi，我是你的AI设计师</h3>
                                <p className="text-sm text-gray-500">让我们开始今天的创作吧！</p>
                            </div>
                        </div>

                        {/* Example Cards */}
                        <div className="space-y-3">
                            {exampleProjects.map((example, index) => (
                                <button
                                    key={index}
                                    onClick={() => handleSendMessage(example.description)}
                                    className="w-full bg-gray-50 hover:bg-gray-100 rounded-2xl p-4 flex items-center justify-between transition-colors text-left"
                                >
                                    <div className="flex-1">
                                        <h4 className="text-sm font-semibold text-gray-900 mb-1">{example.title}</h4>
                                        <p className="text-xs text-gray-500 line-clamp-1">{example.description}</p>
                                    </div>
                                    <div className="text-3xl ml-4">{example.image}</div>
                                </button>
                            ))}
                        </div>

                        {/* Cut Button */}
                        <button className="flex items-center gap-2 text-sm text-gray-400 hover:text-gray-600 transition-colors">
                            <span className="w-4 h-4 border border-gray-300 rounded-full flex items-center justify-center">
                                <span className="w-2 h-2 border-t border-l border-gray-400"></span>
                            </span>
                            <span>切换</span>
                        </button>
                    </div>
                )}

                {messages.map((message, index) => (
                    <div key={message.id} className="space-y-4">
                        {/* Show date separator */}
                        {(index === 0 || formatDate(messages[index - 1].timestamp) !== formatDate(message.timestamp)) && (
                            <div className="text-center my-4">
                                <span className="text-xs text-gray-400">{formatDate(message.timestamp)}</span>
                            </div>
                        )}

                        {message.role === 'user' ? (
                            <div className="flex justify-end">
                                <div className="bg-gray-100 rounded-2xl px-4 py-2.5 max-w-[75%]">
                                    <p className="text-sm text-gray-900">{message.content}</p>
                                </div>
                            </div>
                        ) : (
                            <div className="flex flex-col space-y-2">
                                {message.content && (
                                    <p className="text-sm text-gray-700 leading-relaxed whitespace-pre-wrap">{message.content}</p>
                                )}
                                {message.toolStatus && (
                                    <div className="flex items-center gap-2 text-gray-400">
                                        <Loader2 size={14} className="animate-spin" />
                                        <span className="text-sm">{message.toolStatus}</span>
                                    </div>
                                )}
                                {message.images && message.images.length > 0 && (
                                    <div className="grid grid-cols-2 gap-2">
                                        {message.images.map((url, i) => (
                                            // eslint-disable-next-line @next/next/no-img-element
                                            <img
                                                key={`${message.id}-img-${i}`}
                                                src={url}
                                                alt="生成的图片"
                                                className="w-full h-auto rounded-lg border border-gray-100"
                                            />
                                        ))}
                                    </div>
                                )}
                            </div>
                        )}
                    </div>
                ))}

                {isLoading && (
                    <div className="flex items-center gap-2 text-gray-400">
                        <Loader2 size={14} className="animate-spin" />
                        <span className="text-sm">思考中...</span>
                    </div>
                )}

                <div ref={messagesEndRef} />
            </div>

            {/* Input Area */}
            <div className="border-t border-gray-100 p-4">
                <div className="flex items-center gap-2">
                    <button className="p-2 hover:bg-gray-100 rounded-full transition-colors">
                        <Paperclip size={20} className="text-gray-400" />
                    </button>
                    <button className="p-2 hover:bg-gray-100 rounded-full transition-colors">
                        <AtSign size={20} className="text-gray-400" />
                    </button>

                    <div className="flex-1 relative">
                        <input
                            type="text"
                            value={input}
                            onChange={(e) => setInput(e.target.value)}
                            onKeyDown={(e) => {
                                if (e.key === 'Enter') {
                                    e.preventDefault();
                                    handleSendMessage();
                                }
                            }}
                            placeholder="输入你的设计需求"
                            className="w-full px-4 py-2.5 rounded-full border border-gray-200 focus:border-gray-300 focus:outline-none text-sm bg-gray-50"
                            disabled={isLoading}
                        />
                    </div>

                    <button className="p-2 hover:bg-gray-100 rounded-full transition-colors">
                        <MapPin size={20} className="text-gray-400" />
                    </button>
                    <button className="p-2 hover:bg-gray-100 rounded-full transition-colors">
                        <Zap size={20} className="text-gray-400" />
                    </button>
                    <button className="p-2 hover:bg-gray-100 rounded-full transition-colors">
                        <Globe size={20} className="text-gray-400" />
                    </button>
                    <button
                        onClick={() => handleSendMessage()}
                        disabled={!input.trim() || isLoading}
                        className="p-2.5 bg-blue-500 hover:bg-blue-600 rounded-full transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                    >
                        <ArrowUp size={18} className="text-white" />
                    </button>
                </div>
            </div>
        </div>
    );
}
