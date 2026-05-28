'use client';

import React, { useEffect, useState } from 'react';
import { Plus, Bell } from 'lucide-react';
import Link from 'next/link';
import { useRouter } from 'next/navigation';

import { ProjectCard } from '@/components/lovart/ProjectCard';
import { useAuth } from '@/lib/auth/client';
import {
    getCredits,
    listProjects,
    type Project,
} from '@/lib/api';

export default function ProjectsPage() {
    const router = useRouter();
    const { user, signOut } = useAuth();
    const [projects, setProjects] = useState<Project[]>([]);
    const [nextCursor, setNextCursor] = useState<string | null>(null);
    const [isLoading, setIsLoading] = useState(true);
    const [loadingMore, setLoadingMore] = useState(false);
    const [credits, setCredits] = useState<number | null>(null);

    useEffect(() => {
        let cancelled = false;
        async function loadData() {
            if (!user) {
                setIsLoading(false);
                return;
            }
            try {
                const [list, creditsRow] = await Promise.all([
                    listProjects({ limit: 20 }),
                    getCredits(),
                ]);
                if (cancelled) return;
                setProjects(list.items);
                setNextCursor(list.nextCursor);
                setCredits(creditsRow.credits);
            } catch (error) {
                console.error('Failed to load data:', error);
            } finally {
                if (!cancelled) setIsLoading(false);
            }
        }
        void loadData();
        return () => {
            cancelled = true;
        };
    }, [user]);

    async function handleLoadMore() {
        if (!nextCursor || loadingMore) return;
        setLoadingMore(true);
        try {
            const more = await listProjects({ limit: 20, cursor: nextCursor });
            setProjects((prev) => [...prev, ...more.items]);
            setNextCursor(more.nextCursor);
        } catch (error) {
            console.error('Failed to load more:', error);
        } finally {
            setLoadingMore(false);
        }
    }

    const formatDate = (dateString: string) => {
        const date = new Date(dateString);
        const now = new Date();
        const diffInMs = now.getTime() - date.getTime();
        const diffInMins = Math.floor(diffInMs / 60000);
        const diffInHours = Math.floor(diffInMs / 3600000);
        const diffInDays = Math.floor(diffInMs / 86400000);

        if (diffInMins < 1) return '刚刚编辑';
        if (diffInMins < 60) return `${diffInMins} 分钟前编辑`;
        if (diffInHours < 24) return `${diffInHours} 小时前编辑`;
        if (diffInDays < 7) return `${diffInDays} 天前编辑`;
        return date.toLocaleDateString('zh-CN');
    };

    return (
        <div className="h-screen bg-white text-gray-900 font-sans">
            <main className="h-full flex flex-col overflow-hidden">
                <div className="flex-1 overflow-y-auto">
                    {/* Top Bar */}
                    <div className="flex items-center justify-between px-8 py-4">
                        <div className="flex items-center gap-3">
                            <div className="w-8 h-8 bg-black rounded-full flex items-center justify-center text-white text-sm font-bold">L</div>
                            <span className="text-lg font-semibold text-gray-900">Lovart</span>
                        </div>

                        <div className="flex items-center gap-2">
                            <button className="p-2 hover:bg-gray-100 rounded-lg transition-colors relative">
                                <Bell size={18} className="text-gray-600" />
                                <span className="absolute top-1 right-1 w-1.5 h-1.5 bg-red-500 rounded-full"></span>
                            </button>

                            {user && credits !== null && (
                                <div className="px-3 py-1.5 bg-black text-white rounded-full text-xs font-medium flex items-center gap-1.5">
                                    <span className="text-sm">⚡</span>
                                    <span>{credits.toLocaleString()}</span>
                                </div>
                            )}

                            {user ? (
                                <button
                                    onClick={async () => {
                                        await signOut();
                                        router.replace('/sign-in');
                                    }}
                                    className="px-4 py-2 border border-gray-300 rounded-full text-sm font-medium text-gray-700 hover:bg-gray-50 transition-colors"
                                >
                                    退出登录
                                </button>
                            ) : (
                                <Link
                                    href="/sign-in?next=/lovart/projects"
                                    className="px-4 py-2 bg-black text-white rounded-full text-sm font-medium hover:bg-gray-800 transition-colors"
                                >
                                    登录
                                </Link>
                            )}
                        </div>
                    </div>

                    <div className="px-8 pb-8">
                        {!user ? (
                            <div className="flex flex-col items-center justify-center h-full">
                                <div className="text-center max-w-md">
                                    <h2 className="text-2xl font-bold mb-4">欢迎来到 Lovart</h2>
                                    <p className="text-gray-600 mb-6">登录以查看和管理您的项目</p>
                                    <Link
                                        href="/sign-in?next=/lovart/projects"
                                        className="px-6 py-3 bg-black text-white rounded-full font-medium hover:bg-gray-800 transition-colors"
                                    >
                                        立即登录
                                    </Link>
                                </div>
                            </div>
                        ) : isLoading ? (
                            <div className="flex items-center justify-center h-full">
                                <div className="text-gray-400">加载中...</div>
                            </div>
                        ) : (
                            <div>
                                <div className="flex items-center justify-between mb-6">
                                    <h2 className="text-lg font-semibold text-gray-800">
                                        所有项目
                                        <span className="ml-2 text-sm font-normal text-gray-500">
                                            ({projects.length}
                                            {nextCursor ? '+' : ''})
                                        </span>
                                    </h2>
                                    <Link
                                        href="/lovart/canvas"
                                        className="flex items-center gap-2 px-4 py-2 bg-black text-white rounded-full text-sm font-medium hover:bg-gray-800 transition-colors"
                                    >
                                        <Plus size={18} />
                                        新建项目
                                    </Link>
                                </div>

                                {projects.length === 0 ? (
                                    <div className="flex flex-col items-center justify-center py-20">
                                        <div className="w-20 h-20 bg-gray-100 rounded-full flex items-center justify-center mb-4">
                                            <Plus size={32} className="text-gray-400" />
                                        </div>
                                        <h3 className="text-lg font-medium text-gray-900 mb-2">还没有项目</h3>
                                        <p className="text-gray-500 mb-6">创建您的第一个项目开始设计</p>
                                        <Link
                                            href="/lovart/canvas"
                                            className="px-6 py-3 bg-black text-white rounded-full font-medium hover:bg-gray-800 transition-colors"
                                        >
                                            创建项目
                                        </Link>
                                    </div>
                                ) : (
                                    <>
                                        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-6">
                                            {projects.map((project) => (
                                                <Link key={project.id} href={`/lovart/canvas?id=${project.id}`}>
                                                    <ProjectCard
                                                        title={project.title}
                                                        date={formatDate(project.updated_at)}
                                                        imageUrl={project.thumbnail || undefined}
                                                    />
                                                </Link>
                                            ))}
                                        </div>
                                        {nextCursor && (
                                            <div className="text-center mt-8">
                                                <button
                                                    onClick={handleLoadMore}
                                                    disabled={loadingMore}
                                                    className="px-6 py-2 border border-gray-300 rounded-full text-sm font-medium text-gray-700 hover:bg-gray-50 transition-colors disabled:opacity-50"
                                                >
                                                    {loadingMore ? '加载中…' : '加载更多'}
                                                </button>
                                            </div>
                                        )}
                                    </>
                                )}
                            </div>
                        )}
                    </div>
                </div>
            </main>
        </div>
    );
}
