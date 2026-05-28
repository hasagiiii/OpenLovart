import { NextRequest, NextResponse } from 'next/server';
import OpenAI from 'openai';

import { getServerSession } from '@/lib/auth/server';

export async function POST(request: NextRequest) {
    const session = await getServerSession();
    if (!session) {
        return NextResponse.json({ error: 'unauthenticated' }, { status: 401 });
    }

    try {
        const { prompt } = await request.json();

        if (!prompt || typeof prompt !== 'string') {
            return NextResponse.json(
                { error: 'Prompt is required' },
                { status: 400 }
            );
        }

        const apiKey = process.env.XAI_API_KEY;

        if (!apiKey) {
            return NextResponse.json(
                { error: 'XAI_API_KEY not configured' },
                { status: 500 }
            );
        }

        const client = new OpenAI({
            apiKey: apiKey,
            baseURL: 'https://api.x.ai/v1',
            timeout: 360000,
        });

        const completion = await client.chat.completions.create({
            model: 'grok-4-1-fast-non-reasoning',
            messages: [
                {
                    role: 'system',
                    content:
                        "You are a professional design assistant. Based on user's description, provide detailed design suggestions including layout, colors, typography, and visual elements. Be specific and creative.",
                },
                {
                    role: 'user',
                    content: `Create a design concept for: ${prompt}`,
                },
            ],
            user: session.user.id,
        });

        const designSuggestion = completion.choices[0].message.content;

        return NextResponse.json({
            suggestion: designSuggestion,
        });
    } catch (error) {
        console.error('Error generating design:', error);
        const message =
            error instanceof Error ? error.message : 'Unknown error';
        return NextResponse.json(
            { error: 'Failed to generate design', details: message },
            { status: 500 }
        );
    }
}
