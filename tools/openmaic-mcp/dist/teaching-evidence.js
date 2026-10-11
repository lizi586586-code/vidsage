const allowedSkills = new Set(['feynman-learning', 'workshop-style']);
// Consume durable tool-result frames only. Assistant prose and tool calls
// are not proof that a skill's complete instructions reached the model.
export function loadedTeachingSkills(events, recommended) {
    if (!recommended.length || recommended.some(skill => !allowedSkills.has(skill)) || new Set(recommended).size !== recommended.length)
        throw new Error('invalid_teaching_plan');
    const loaded = new Map();
    for (const event of events) {
        const frame = event;
        if (frame?.type !== 'message_end')
            continue;
        const message = frame.data?.message;
        const details = message?.details;
        if (message?.role !== 'toolResult' || message.toolName !== 'read' || message.isError !== false || !details?.skill || !recommended.includes(details.skill))
            continue;
        if (details.path !== `/app/skills/agent-runtime/${details.skill}/SKILL.md` || details.offset !== 1 || !Number.isInteger(details.totalLines) || !details.totalLines || details.totalLines < 1 || !Number.isInteger(details.lines) || details.lines < details.totalLines || typeof details.sourceHash !== 'string' || !/^[a-f0-9]{16,64}$/.test(details.sourceHash))
            continue;
        loaded.set(details.skill, { skill: details.skill, source_hash: details.sourceHash });
    }
    if (recommended.some(skill => !loaded.has(skill)))
        throw new Error('skill_loading_unverified');
    return recommended.map(skill => loaded.get(skill));
}
// Return references for the worker to verify as real audio responses. Having
// these references alone must never mark narration verified or publish a link.
export function narrationReferences(document, stageID) {
    if (!/^[A-Za-z0-9_-]{1,128}$/.test(stageID))
        throw new Error('invalid_generation_result');
    const doc = document;
    if (doc?.stage?.id !== stageID || !Array.isArray(doc.scenes) || !doc.scenes.length)
        throw new Error('invalid_generation_result');
    const references = new Set();
    for (const scene of doc.scenes) {
        if (!scene || Array.isArray(scene) || scene.stageId !== stageID || !Array.isArray(scene.actions) || scene.actions.some(action => !action || typeof action !== 'object' || Array.isArray(action)))
            throw new Error('invalid_generation_result');
        if (scene.actions.some(action => action.type === 'speech' && (typeof action.text !== 'string' || !action.text.trim())))
            throw new Error('invalid_generation_result');
        const speeches = scene.actions.filter(action => action.type === 'speech');
        if (!speeches.length)
            throw new Error('narration_incomplete');
        for (const speech of speeches) {
            if (typeof speech.audioUrl !== 'string')
                throw new Error('narration_incomplete');
            let url;
            try {
                url = new URL(speech.audioUrl, 'https://open.maic.chat');
            }
            catch {
                throw new Error('invalid_audio_reference');
            }
            const prefix = `/api/classroom-media/${stageID}/`;
            if (url.origin !== 'https://open.maic.chat' || url.username || url.password || url.search || url.hash || !url.pathname.startsWith(prefix) || url.pathname.length <= prefix.length || /%|\.\./.test(url.pathname))
                throw new Error('invalid_audio_reference');
            references.add(url.pathname);
        }
    }
    return [...references];
}
