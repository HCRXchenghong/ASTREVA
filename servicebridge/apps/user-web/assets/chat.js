/* Chat page runtime (external for strict CSP). */
        // === B13：按当前语言同步文档书写方向（阿拉伯语 RTL） ===
        (function applyDocumentDirection() {
            const lang = (window.__CHAT_LANG__ || 'zh').toLowerCase();
            const isRTL = lang === 'ar';
            document.documentElement.setAttribute('dir', isRTL ? 'rtl' : 'ltr');
            document.documentElement.setAttribute(
                'lang',
                isRTL ? 'ar' : (lang === 'en' ? 'en' : 'zh-CN')
            );
        })();

        const AVATAR_ASSETS = Object.freeze({
            service: './assets/img/avatar-service.png',
            visitor: Object.freeze({
                zh: './assets/img/avatar-visitor-zh.png',
                en: './assets/img/avatar-visitor-en.png',
                ar: './assets/img/avatar-visitor-ar.png'
            })
        });

        function visitorAvatar() {
            return AVATAR_ASSETS.visitor[window.__CHAT_LANG__] || AVATAR_ASSETS.visitor.zh;
        }

        // === B12：客服姓名与头像统一取自会话下发的 assigned_agent_* 字段 ===
        function agentAvatarURL() {
            const assigned = String(State.assignedAgentAvatar || '').trim();
            return assigned ? resolveAssetURL(assigned) : AVATAR_ASSETS.service;
        }

        function agentDisplayName() {
            const assigned = String(State.assignedAgentName || '').trim();
            if (assigned) return assigned;
            return State.sessionType === 'HUMAN' ? t('humanAgentFallback') : t('agentNameDefault');
        }

        // 把会话对象里的客服信息写入 State，缺失时回退到默认文案/头像。
        function applyAssignedAgent(conversation) {
            if (!conversation) return;
            const name = String(conversation.assigned_agent_name || '').trim();
            const avatar = String(conversation.assigned_agent_avatar || '').trim();
            State.assignedAgentName = name;
            State.assignedAgentAvatar = avatar;
        }

        // === 状态管理 ===
        const State = {
            sessionType: 'AI', // 'AI', 'QUEUING', 'HUMAN', 'ENDED'
            agentName: t('agentNameDefault'),
            agentAvatar: AVATAR_ASSETS.service,
            // B12：真实客服信息（来自 assigned_agent_name / assigned_agent_avatar）。
            assignedAgentName: '',
            assignedAgentAvatar: '',
            // B11：是否已提交转人工请求（等待人工接入）。
            humanRequested: false,
            // B14：恢复已结束会话时保留历史，不直接踢回入口页。
            keepHistoryOnEnd: false,
            // B15：等待服务端回执的本地消息（client_msg_id -> { timer }）。
            pendingSends: new Map(),
            soundEnabled: true,
            activePanel: null, // 'emoji', 'more', or null
            entryVisible: true,
            startingConversation: false,
            pendingReturnToEntry: false,
            unreadCount: 0,
            apiBase: inferAPIBase(),
            ws: null,
            visitorToken: '',
            conversationId: '',
            connected: false,
            selectedRatingScore: 0,
            ratingSubmitted: false,
            ratingSubmitting: false,
            ratingPromptVisible: false,
            evalReturnMessage: '',
            renderedServerMsgIds: new Set(),
            renderedClientMsgIds: new Set(),
            hasMoreHistory: false,
            nextBefore: '',
            loadingHistory: false,
            pendingInitialMessages: [],
            quickRepliesRendered: false,
            quickReplies: [],
            mediaRecorder: null,
            recordingStream: null,
            recordingChunks: [],
            isRecording: false,
            recordingCanceled: false,
            recordingStartY: 0,
            recordingStartedAt: 0,
            recordGestureSource: '',
            recordPointerId: null,
            recordPendingStop: false,
            recordPendingForceCancel: false,
            activeAudioElement: null,
            activeAudioButton: null,
            typingTimer: null,
            typingSender: '',
            contacts: {
                telephone: '400-123-4567',
                whatsapp: '+86 400 888 8888',
                telegram: '@AstrevaSupport'
            }
        };

        const DOM = {
            entryView: document.getElementById('entry-view'),
            entryBtn: document.getElementById('entry-btn'),
            chatShell: document.getElementById('chat-shell'),
            chatList: document.getElementById('chat-list'),
            input: document.getElementById('chat-input'),
            btnPlus: document.getElementById('btn-plus'),
            btnSend: document.getElementById('btn-send'),
            panelEmoji: document.getElementById('panel-emoji'),
            panelMore: document.getElementById('panel-more'),
            emojiContainer: document.getElementById('emoji-container'),
            btnVoice: document.getElementById('btn-voice'),
            sysBanner: document.getElementById('sys-banner'),
            sysBannerText: document.getElementById('sys-banner-text'),
            headerTitle: document.getElementById('header-title'),
            headerTyping: document.getElementById('header-typing'),
            headerAiBadge: document.getElementById('header-ai-badge'),
            toast: document.getElementById('toast'),
            recordOverlay: document.getElementById('record-overlay'),
            recordOverlayCard: document.getElementById('record-overlay-card'),
            recordOverlayText: document.getElementById('record-overlay-text'),
            recordOverlayTip: document.getElementById('record-overlay-tip'),
            recordOverlayCancel: document.getElementById('record-overlay-cancel'),
            recordOverlayCancelText: document.getElementById('record-overlay-cancel-text'),
            evalOverlay: document.getElementById('eval-overlay'),
            evalSheet: document.getElementById('eval-sheet'),
            evalAdviceBox: document.getElementById('eval-advice-box'),
            evalAdvice: document.getElementById('eval-advice'),
            endConfirmOverlay: document.getElementById('end-confirm-overlay'),
            endConfirmSheet: document.getElementById('end-confirm-sheet'),
            endConfirmCancel: document.getElementById('end-confirm-cancel'),
            endConfirmSubmit: document.getElementById('end-confirm-submit'),
            sound: document.getElementById('sound-new-msg'),
            btnSound: document.getElementById('btn-sound'),
            uploadImageInput: document.getElementById('upload-image-input'),
            loadHistory: document.getElementById('load-history'),
            historyAnchor: document.getElementById('history-anchor')
        };
        const INITIAL_CHAT_HTML = DOM.chatList.innerHTML;
        const DEFAULT_SYS_BANNER_CLASS = DOM.sysBanner.className;
        const STORAGE_KEYS = {
            visitorSession: 'customer_service_visitor_session_v1'
        };
        // 官网埋点（admin-server）在访客同意后写入本域 localStorage 的访客 ID，
        // 聊天挂件把它作为 visitor_ref 交给引擎，作为画像聚合键。
        const VISITOR_REF_KEY = 'astreva_visitor_id';
        const ACTIVE_SESSION_TYPES = new Set(['AI', 'QUEUING', 'HUMAN']);

        // 常用 Emoji 列表
        const EMOJIS = ['😀', '😃', '😄', '😁', '😆', '😅', '😂', '🤣', '😊', '😇', '🙂', '🙃', '😉', '😌', '😍', '🥰', '😘', '😗', '😙', '😚', '😋', '😛', '😝', '😜', '🤪', '🤨', '🧐', '🤓', '😎', '🤩', '🥳', '😏', '😒', '😞', '😔', '😟', '😕', '🙁', '☹️', '😣'];

        // === 工具函数 ===
        function inferAPIBase() {
            const configured = document.querySelector('meta[name="customer-service-api-base"]')?.content || window.CUSTOMER_SERVICE_API_BASE || '';
            if (configured.trim()) return configured.trim().replace(/\/$/, '');
            if (location.protocol === 'file:') return 'http://127.0.0.1:8080';
            if (location.port && location.port !== '8080') return `${location.protocol}//${location.hostname}:8080`;
            return location.origin;
        }

        function inferWSBase() {
            const apiUrl = new URL(State.apiBase);
            apiUrl.protocol = apiUrl.protocol === 'https:' ? 'wss:' : 'ws:';
            return apiUrl.origin;
        }

        function showEntryView() {
            State.entryVisible = true;
            DOM.chatShell.classList.add('hidden');
            DOM.entryView.classList.remove('hidden');
            DOM.entryView.classList.add('flex');
        }

        function showChatView() {
            State.entryVisible = false;
            DOM.entryView.classList.remove('flex');
            DOM.entryView.classList.add('hidden');
            DOM.chatShell.classList.remove('hidden');
        }

        function clearVisitorSession() {
            try {
                window.localStorage.removeItem(STORAGE_KEYS.visitorSession);
            } catch (err) {}
            notifyHostSessionClear();
        }

        function loadVisitorSession() {
            try {
                const raw = window.localStorage.getItem(STORAGE_KEYS.visitorSession);
                if (!raw) return null;
                const parsed = JSON.parse(raw);
                const visitorToken = String(parsed?.visitorToken || '').trim();
                const conversationId = String(parsed?.conversationId || '').trim();
                // 历史版本可能保存过 QUEUING，现在排队期间一律按 AI 接待展示（无痕转人工）。
                const rawSessionType = ACTIVE_SESSION_TYPES.has(parsed?.sessionType) ? parsed.sessionType : 'AI';
                const sessionType = rawSessionType === 'QUEUING' ? 'AI' : rawSessionType;
                if (!visitorToken || !conversationId) {
                    clearVisitorSession();
                    return null;
                }
                return {
                    visitorToken,
                    conversationId,
                    sessionType
                };
            } catch (err) {
                clearVisitorSession();
                return null;
            }
        }

        // 读取官网埋点写入的访客 ID（astreva_visitor_id）作为画像聚合键。
        // 未同意（官网未写入该键）、不存在或格式非法时返回空串，调用方不得携带 visitor_ref。
        // 校验规则与后端 cleanProfileVisitorID 保持一致：<=64 个 [A-Za-z0-9_.-] 字符。
        function readVisitorRef(storage) {
            try {
                const store = storage || window.localStorage;
                const raw = store.getItem(VISITOR_REF_KEY);
                if (typeof raw !== 'string') return '';
                const value = raw.trim();
                if (!value || value.length > 64 || !/^[A-Za-z0-9_.-]+$/.test(value)) return '';
                return value;
            } catch (err) {
                return '';
            }
        }

        function saveVisitorSession() {
            if (!State.visitorToken || !State.conversationId || State.entryVisible || !ACTIVE_SESSION_TYPES.has(State.sessionType)) {
                return;
            }
            const payload = {
                visitorToken: State.visitorToken,
                conversationId: State.conversationId,
                sessionType: State.sessionType,
                savedAt: Date.now()
            };
            try {
                window.localStorage.setItem(STORAGE_KEYS.visitorSession, JSON.stringify(payload));
            } catch (err) {}
            notifyHostSessionSave(payload);
        }

        function escapeHTML(value) {
            return String(value)
                .replaceAll('&', '&amp;')
                .replaceAll('<', '&lt;')
                .replaceAll('>', '&gt;')
                .replaceAll('"', '&quot;')
                .replaceAll("'", '&#39;');
        }

        function resolveAssetURL(value) {
            value = String(value || '');
            if (!value) return '';
            if (/^https?:\/\//i.test(value) || value.startsWith('data:')) return value;
            if (value.startsWith('/')) return State.apiBase.replace(/\/$/, '') + value;
            return value;
        }

        function safeImageSrc(value) {
            const resolved = resolveAssetURL(value);
            if (/^data:image\/(png|jpe?g|gif|webp);base64,/i.test(resolved)) return resolved;
            try {
                const parsed = new URL(resolved, location.href);
                if (parsed.protocol === 'http:' || parsed.protocol === 'https:') return parsed.href;
            } catch (err) {}
            return '';
        }

        function clientMsgId() {
            return 'web_' + Date.now().toString(36) + '_' + Math.random().toString(36).slice(2);
        }

        function showToast(msg, duration = 2000) {
            DOM.toast.textContent = msg;
            gsap.to(DOM.toast, { opacity: 1, duration: 0.2 });
            setTimeout(() => gsap.to(DOM.toast, { opacity: 0, duration: 0.3 }), duration);
        }

        function scrollToBottom() {
            setTimeout(() => {
                DOM.chatList.scrollTo({ top: DOM.chatList.scrollHeight, behavior: 'smooth' });
            }, 50);
        }

        function playSound() {
            if (State.soundEnabled && State.sessionType !== 'ENDED') {
                DOM.sound.currentTime = 0;
                DOM.sound.play().catch(() => {});
            }
        }

        function isEmbedMode() {
            return document.documentElement.classList.contains('embed-mode');
        }

        function notifyHost(type, payload = {}) {
            if (!isEmbedMode() || window.parent === window) return;
            const target = hostTargetOrigin();
            if (!target) return;
            window.parent.postMessage({ type, ...payload }, target);
        }

        // === 主站一级存储桥 ===
        // 聊天窗口是跨站 iframe（api.astreva.ltd），其 localStorage 会被 Safari ITP /
        // 隐私模式等第三方存储拦截，导致刷新后丢会话。这里通过 postMessage 请主站
        // 页面（第一方）代为保存访客会话令牌，刷新后再向主站取回，即可恢复会话与历史。
        function allowedParentOrigins() {
            const raw = window.__SERVICEBRIDGE_ALLOWED_PARENT__;
            const list = Array.isArray(raw) ? raw : [raw];
            const origins = [];
            for (const item of list) {
                try {
                    const configured = new URL(String(item || ''));
                    if (configured.protocol === 'http:' || configured.protocol === 'https:') origins.push(configured.origin);
                } catch (err) {}
            }
            return origins;
        }

        // 只向官网白名单 Origin 发送访客会话 token；绝不使用 '*' 兜底。
        // 使用 ancestorOrigins 或 referrer 校验父页面；两者都不可用时宁可不做 token 桥。
        function hostTargetOrigin() {
            const allowed = allowedParentOrigins();
            if (!allowed.length) return '';
            let ancestor = '';
            try {
                const ancestors = window.location.ancestorOrigins;
                if (ancestors && ancestors.length) ancestor = ancestors[0];
            } catch (err) {}
            if (!ancestor) {
                try { ancestor = new URL(document.referrer).origin; } catch (err) {}
            }
            return allowed.includes(ancestor) ? ancestor : '';
        }

        function isFramed() {
            try { return window.self !== window.top; } catch (err) { return true; }
        }

        function notifyHostSessionSave(session) {
            if (!isFramed() || window.parent === window) return;
            const target = hostTargetOrigin();
            if (!target) return;
            try {
                window.parent.postMessage({ type: 'servicebridge:session-put', session }, target);
            } catch (err) {}
        }

        function notifyHostSessionClear() {
            if (!isFramed() || window.parent === window) return;
            const target = hostTargetOrigin();
            if (!target) return;
            try {
                window.parent.postMessage({ type: 'servicebridge:session-clear' }, target);
            } catch (err) {}
        }

        // 向主站请求访客会话；超时或主站未托管时返回 null。
        function requestHostSession(timeoutMs) {
            return new Promise((resolve) => {
                if (!isFramed() || window.parent === window) { resolve(null); return; }
                const target = hostTargetOrigin();
                if (!target) { resolve(null); return; }
                let settled = false;
                const finish = (session) => {
                    if (settled) return;
                    settled = true;
                    window.removeEventListener('message', onMessage);
                    resolve(session);
                };
                const onMessage = (event) => {
                    if (event.source !== window.parent || event.origin !== target) return;
                    const data = event.data || {};
                    if (data.type !== 'servicebridge:session-restore') return;
                    const raw = data.session;
                    const visitorToken = String(raw?.visitorToken || '').trim();
                    const conversationId = String(raw?.conversationId || '').trim();
                    if (!visitorToken || !conversationId) { finish(null); return; }
                    let sessionType = ACTIVE_SESSION_TYPES.has(raw?.sessionType) ? raw.sessionType : 'AI';
                    if (sessionType === 'QUEUING') sessionType = 'AI';
                    finish({ visitorToken, conversationId, sessionType });
                };
                window.addEventListener('message', onMessage);
                try {
                    window.parent.postMessage({ type: 'servicebridge:session-get' }, target);
                } catch (err) {}
                setTimeout(() => finish(null), timeoutMs || 800);
            });
        }

        function resetSystemBanner() {
            DOM.sysBanner.className = DEFAULT_SYS_BANNER_CLASS;
            DOM.sysBanner.classList.add('hidden');
            DOM.sysBannerText.textContent = t('notice');
        }

        function refreshChatRefs() {
            DOM.loadHistory = document.getElementById('load-history');
            DOM.historyAnchor = document.getElementById('history-anchor');
        }

        function hideEvalImmediately() {
            DOM.evalOverlay.classList.remove('flex');
            DOM.evalOverlay.classList.add('hidden');
            gsap.set(DOM.evalSheet, { y: '100%' });
        }

        function hideEndConfirmImmediately() {
            DOM.endConfirmOverlay.classList.remove('flex');
            DOM.endConfirmOverlay.classList.add('hidden');
            gsap.set(DOM.endConfirmOverlay, { opacity: 0 });
            gsap.set(DOM.endConfirmSheet, { y: '100%' });
        }

        function showEndConfirm() {
            if (State.sessionType === 'ENDED' || State.pendingReturnToEntry) return;
            closeAllPanels();
            DOM.endConfirmOverlay.classList.remove('hidden');
            DOM.endConfirmOverlay.classList.add('flex');
            gsap.killTweensOf([DOM.endConfirmOverlay, DOM.endConfirmSheet]);
            gsap.set(DOM.endConfirmSheet, { y: '100%' });
            gsap.to(DOM.endConfirmOverlay, { opacity: 1, duration: 0.18, ease: "power1.out" });
            gsap.to(DOM.endConfirmSheet, { y: 0, duration: 0.28, ease: "power3.out" });
        }

        function closeEndConfirm(onComplete = null) {
            if (DOM.endConfirmOverlay.classList.contains('hidden')) {
                if (typeof onComplete === 'function') onComplete();
                return;
            }
            gsap.killTweensOf([DOM.endConfirmOverlay, DOM.endConfirmSheet]);
            gsap.to(DOM.endConfirmOverlay, { opacity: 0, duration: 0.16, ease: "power1.in" });
            gsap.to(DOM.endConfirmSheet, {
                y: '100%',
                duration: 0.2,
                ease: "power2.in",
                onComplete: () => {
                    DOM.endConfirmOverlay.classList.remove('flex');
                    DOM.endConfirmOverlay.classList.add('hidden');
                    if (typeof onComplete === 'function') onComplete();
                }
            });
        }

        function confirmEndConversation() {
            closeEndConfirm(() => {
                triggerAction('end');
            });
        }

        function stopRecordingStream() {
            if (!State.recordingStream) return;
            State.recordingStream.getTracks().forEach((track) => track.stop());
            State.recordingStream = null;
        }

        function hideRecordingOverlay() {
            DOM.recordOverlay.classList.remove('flex');
            DOM.recordOverlay.classList.add('hidden');
            DOM.recordOverlayCard.classList.remove('record-cancel');
            DOM.recordOverlayCancel.classList.remove('record-cancel-active');
            DOM.recordOverlayText.textContent = t('releaseToSend');
            DOM.recordOverlayTip.textContent = t('slideUpToCancelArea');
            DOM.recordOverlayCancelText.textContent = t('slideUpToCancel');
            DOM.btnVoice.classList.remove('recording');
        }

        function updateRecordingOverlay(isCancel) {
            DOM.recordOverlay.classList.remove('hidden');
            DOM.recordOverlay.classList.add('flex');
            DOM.recordOverlayCard.classList.toggle('record-cancel', !!isCancel);
            DOM.recordOverlayCancel.classList.toggle('record-cancel-active', !!isCancel);
            DOM.recordOverlayText.textContent = isCancel ? t('releaseToCancelSend') : t('releaseToSend');
            DOM.recordOverlayTip.textContent = isCancel ? t('inCancelArea') : t('slideUpToCancelArea');
            DOM.recordOverlayCancelText.textContent = isCancel ? t('releaseToCancel') : t('slideUpToCancel');
            DOM.btnVoice.classList.add('recording');
        }

        function voicePoint(event) {
            if (event && event.touches && event.touches[0]) {
                return { x: event.touches[0].clientX || 0, y: event.touches[0].clientY || 0 };
            }
            if (event && event.changedTouches && event.changedTouches[0]) {
                return { x: event.changedTouches[0].clientX || 0, y: event.changedTouches[0].clientY || 0 };
            }
            return { x: event?.clientX || 0, y: event?.clientY || 0 };
        }

        function isWithinCancelZone(clientX, clientY) {
            if (!clientX || !clientY || DOM.recordOverlay.classList.contains('hidden')) return false;
            const rect = DOM.recordOverlayCancel.getBoundingClientRect();
            const expand = 18;
            return clientX >= rect.left - expand &&
                clientX <= rect.right + expand &&
                clientY >= rect.top - expand &&
                clientY <= rect.bottom + expand;
        }

        function beginVoiceGesture(event, source) {
            if (State.sessionType === 'ENDED' || State.recordGestureSource) return;
            const point = voicePoint(event);
            State.recordGestureSource = source;
            State.recordPointerId = source === 'pointer' ? event.pointerId ?? null : null;
            State.recordPendingStop = false;
            State.recordPendingForceCancel = false;
            State.recordingCanceled = false;
            State.recordingStartY = point.y || 0;
            if (source === 'pointer' && typeof DOM.btnVoice.setPointerCapture === 'function' && event.pointerId != null) {
                try {
                    DOM.btnVoice.setPointerCapture(event.pointerId);
                } catch (err) {}
            }
            event.preventDefault();
            startVoiceRecording(point.y || 0);
        }

        function setAudioBubbleState(button, isPlaying) {
            if (!button) return;
            button.classList.toggle('voice-playing', !!isPlaying);
            const icon = button.querySelector('[data-voice-icon]');
            if (icon) {
                icon.className = isPlaying ? 'fas fa-pause' : 'fas fa-play';
            }
        }

        function stopCurrentAudio(resetProgress = false) {
            if (!State.activeAudioElement) return;
            State.activeAudioElement.pause();
            if (resetProgress) {
                try {
                    State.activeAudioElement.currentTime = 0;
                } catch (err) {}
            }
            setAudioBubbleState(State.activeAudioButton, false);
            State.activeAudioElement = null;
            State.activeAudioButton = null;
        }

        function bindAudioBubbleActions(root) {
            root.querySelectorAll('[data-audio-bubble]').forEach((button) => {
                if (button.dataset.boundAudio === '1') return;
                button.dataset.boundAudio = '1';
                const wrapper = button.closest('.bubble-audio');
                const audio = wrapper ? wrapper.querySelector('.voice-audio-element') : null;
                const durationText = button.querySelector('[data-audio-duration]');
                if (!audio) return;
                const syncDuration = () => {
                    if (!durationText) return;
                    if (Number.isFinite(audio.duration) && audio.duration > 0) {
                        durationText.textContent = `${Math.max(1, Math.round(audio.duration))}"`;
                    } else {
                        durationText.textContent = t('voice');
                    }
                };
                audio.addEventListener('loadedmetadata', syncDuration);
                audio.addEventListener('ended', () => {
                    if (State.activeAudioElement === audio) {
                        State.activeAudioElement = null;
                        State.activeAudioButton = null;
                    }
                    setAudioBubbleState(button, false);
                });
                audio.addEventListener('pause', () => {
                    if (!audio.ended) {
                        setAudioBubbleState(button, false);
                    }
                });
                if (audio.readyState >= 1) {
                    syncDuration();
                }
                button.addEventListener('click', async () => {
                    if (State.activeAudioElement && State.activeAudioElement !== audio) {
                        stopCurrentAudio(true);
                    }
                    if (audio.paused) {
                        try {
                            State.activeAudioElement = audio;
                            State.activeAudioButton = button;
                            setAudioBubbleState(button, true);
                            await audio.play();
                        } catch (err) {
                            console.error(err);
                            stopCurrentAudio(false);
                            showToast(t('voicePlayFailed'));
                        }
                        return;
                    }
                    stopCurrentAudio(true);
                });
            });
        }

        function messageDataAttrs(meta = {}) {
            const attrs = [];
            if (meta.serverMsgId) attrs.push(`data-server-msg-id="${escapeHTML(meta.serverMsgId)}"`);
            if (meta.clientMsgId) attrs.push(`data-client-msg-id="${escapeHTML(meta.clientMsgId)}"`);
            // B15：失败重发所需的元数据。
            if (meta.msgType) attrs.push(`data-msg-type="${escapeHTML(meta.msgType)}"`);
            if (meta.displayText) attrs.push(`data-display-text="${escapeHTML(meta.displayText)}"`);
            if (meta.retryText) attrs.push(`data-retry-text="${escapeHTML(meta.retryText)}"`);
            return attrs.join(' ');
        }

        // B15：失败消息统一的重试图标。
        function retryIconHTML(retryText) {
            const safeRetryText = escapeHTML(retryText).replace(/\n/g, '&#10;');
            return `<i class="fas fa-exclamation-circle text-red-500 text-xl mx-2 cursor-pointer retry-icon" role="button" tabindex="0" title="${escapeHTML(t('tapToRetry'))}" aria-label="${escapeHTML(t('tapToRetry'))}" data-retry-text="${safeRetryText}"></i>`;
        }

        function findMessageElement(meta = {}) {
            const items = DOM.chatList.querySelectorAll('.msg-wrapper');
            for (const item of items) {
                if (meta.serverMsgId && item.dataset.serverMsgId === meta.serverMsgId) return item;
                if (meta.clientMsgId && item.dataset.clientMsgId === meta.clientMsgId) return item;
            }
            return null;
        }

        function resetConversationRuntime() {
            State.pendingReturnToEntry = false;
            State.connected = false;
            State.ws = null;
            State.visitorToken = '';
            State.conversationId = '';
            // B11/B12/B14：清理转人工、真实客服信息与保留历史状态。
            State.humanRequested = false;
            State.keepHistoryOnEnd = false;
            State.assignedAgentName = '';
            State.assignedAgentAvatar = '';
            State.pendingSends.forEach((timer) => clearTimeout(timer));
            State.pendingSends = new Map();
            State.selectedRatingScore = 0;
            State.ratingSubmitted = false;
            State.ratingSubmitting = false;
            State.ratingPromptVisible = false;
            State.evalReturnMessage = '';
            State.renderedServerMsgIds = new Set();
            State.renderedClientMsgIds = new Set();
            State.hasMoreHistory = false;
            State.nextBefore = '';
            State.loadingHistory = false;
            State.pendingInitialMessages = [];
            State.quickRepliesRendered = false;
            State.quickReplies = [];
            State.mediaRecorder = null;
            State.recordingChunks = [];
            State.isRecording = false;
            State.recordingCanceled = false;
            State.recordingStartY = 0;
            State.recordingStartedAt = 0;
            State.recordGestureSource = '';
            State.recordPointerId = null;
            State.recordPendingStop = false;
            State.recordPendingForceCancel = false;
            stopCurrentAudio(true);
            stopRecordingStream();
            hideRecordingOverlay();
        }

        function resetChatView() {
            DOM.chatList.innerHTML = INITIAL_CHAT_HTML;
            refreshChatRefs();
            DOM.input.value = '';
            DOM.input.disabled = false;
            DOM.input.placeholder = '';
            DOM.input.style.height = '40px';
            DOM.btnPlus.style.display = 'flex';
            DOM.btnSend.style.display = 'none';
            hideTypingIndicator();
            DOM.headerTyping.classList.add('hidden');
            DOM.uploadImageInput.value = '';
            closeAllPanels();
            resetSystemBanner();
            hideEndConfirmImmediately();
            DOM.chatList.scrollTop = 0;
            if (DOM.loadHistory) DOM.loadHistory.textContent = t('loadMoreHistory');
        }

        function disconnectSocket() {
            if (!State.ws) return;
            const socket = State.ws;
            State.ws = null;
            socket.onclose = null;
            socket.onerror = null;
            socket.onmessage = null;
            try {
                socket.close();
            } catch (err) {}
        }

        function returnToEntry(message = '') {
            disconnectSocket();
            clearVisitorSession();
            resetConversationRuntime();
            resetChatView();
            hideEvalImmediately();
            updateSessionState('AI');
            State.startingConversation = false;
            DOM.entryBtn.disabled = false;
            DOM.entryBtn.textContent = t('consultService');
            if (isEmbedMode()) {
                showChatView();
                if (message) appendSystemMessage(message);
                return;
            }
            showEntryView();
            if (message) showToast(message);
        }

        function promptForRatingOrReturn(message) {
            State.pendingReturnToEntry = false;
            State.connected = false;
            disconnectSocket();
            updateSessionState('ENDED');
            if (!State.visitorToken || !State.conversationId || State.ratingSubmitted) {
                if (isEmbedMode()) {
                    clearVisitorSession();
                    notifyHost('servicebridge:conversation-closed', { conversationId: State.conversationId });
                }
                returnToEntry(message);
                return;
            }
            State.evalReturnMessage = message;
            showEvalSheet();
        }

        async function openConsultation() {
            if (State.startingConversation) return;
            State.startingConversation = true;
            DOM.entryBtn.disabled = true;
            DOM.entryBtn.textContent = t('connecting');
            clearVisitorSession();
            resetConversationRuntime();
            resetChatView();
            updateSessionState('AI');
            if (isEmbedMode()) {
                showChatView();
            }
            const ok = await initConversation();
            State.startingConversation = false;
            DOM.entryBtn.disabled = false;
            DOM.entryBtn.textContent = t('consultService');
            if (!ok) return;
            showChatView();
            saveVisitorSession();
            scrollToBottom();
        }

        async function restoreConsultation(savedSession) {
            const saved = savedSession || loadVisitorSession();
            if (!saved) return false;
            disconnectSocket();
            resetConversationRuntime();
            resetChatView();
            hideEvalImmediately();
            updateSessionState('AI');
            await loadContactSettings();
            State.visitorToken = saved.visitorToken;
            State.conversationId = saved.conversationId;
            // 恢复旧会话时，把当前网站语言同步给后端：
            // 会话语言是创建时记录的，访客切换网站语言后若不同步，
            // AI 会继续按旧语言（如中文）回复。
            // B14：该接口同时返回会话状态，用于判断历史会话是否已结束。
            const restoredConversation = await syncVisitorLangToServer();
            if (restoredConversation) applyAssignedAgent(restoredConversation);
            State.startingConversation = false;
            DOM.entryBtn.disabled = false;
            DOM.entryBtn.textContent = t('consultService');
            showChatView();
            updateSessionState(saved.sessionType || 'AI');
            let syncState = null;
            try {
                syncState = await syncMessages();
            } catch (err) {
                console.error(err);
            }
            if (syncState === false) {
                returnToEntry(t("sessionExpired"));
                return false;
            }
            if (!State.quickRepliesRendered) {
                renderQuickRepliesCard();
            }
            // B14：会话已结束时保留历史渲染，展示"会话已结束"状态与评价入口。
            if (restoredConversation && restoredConversation.status === 'closed') {
                enterEndedSessionState({ keepHistory: true });
                scrollToBottom();
                return true;
            }
            saveVisitorSession();
            connectSocket();
            scrollToBottom();
            return true;
        }

        async function bootApp() {
            let savedSession = loadVisitorSession();
            // iframe 自身存储被拦截/清空时，向主站第一方存储取回会话令牌。
            if (!savedSession && isEmbedMode()) {
                savedSession = await requestHostSession();
            }
            if (savedSession) {
                const restored = await restoreConsultation(savedSession);
                if (restored) return;
            }
            if (isEmbedMode()) {
                await openConsultation();
                return;
            }
            returnToEntry();
        }

        // === 核心 UI 逻辑 ===

        // 输入框高度自动调整与发送按钮切换
        DOM.input.addEventListener('input', function() {
            // 高度自适应
            this.style.height = '40px';
            this.style.height = (this.scrollHeight) + 'px';
            
            // 切换 发送 / + 按钮
            if (this.value.trim().length > 0) {
                DOM.btnPlus.style.display = 'none';
                DOM.btnSend.style.display = 'flex';
            } else {
                DOM.btnPlus.style.display = 'flex';
                DOM.btnSend.style.display = 'none';
            }
        });

        // 底部面板展开/收起控制
        function togglePanel(panelName) {
            const panelEmoji = document.getElementById('panel-emoji');
            const panelMore = document.getElementById('panel-more');
            
            if (State.activePanel === panelName) {
                // 关闭当前
                closeAllPanels();
            } else {
                // 打开指定面板
                State.activePanel = panelName;
                if (panelName === 'emoji') {
                    gsap.to(panelMore, { height: 0, duration: 0.2 });
                    gsap.to(panelEmoji, { height: 250, duration: 0.3, ease: "power2.out" });
                } else if (panelName === 'more') {
                    gsap.to(panelEmoji, { height: 0, duration: 0.2 });
                    gsap.to(panelMore, { height: 250, duration: 0.3, ease: "power2.out" });
                }
                scrollToBottom();
            }
        }

        function closeAllPanels() {
            State.activePanel = null;
            gsap.to('.bottom-panel', { height: 0, duration: 0.2 });
        }

        // 初始化 Emoji
        function initEmojis() {
            EMOJIS.forEach(emo => {
                const span = document.createElement('span');
                span.className = 'text-2xl cursor-pointer text-center hover:bg-gray-200 py-1 rounded transition-colors';
                span.textContent = emo;
                span.onclick = () => {
                    DOM.input.value += emo;
                    DOM.input.dispatchEvent(new Event('input')); // 触发高度和按钮切换逻辑
                };
                DOM.emojiContainer.appendChild(span);
            });
        }

        // === 消息构建与收发逻辑 ===

        // 构建普通文本消息
        function createMessageHTML(text, isUser = false, isFailed = false, meta = {}, extraBubbleClass = '', aiTag = false) {
            const safeText = escapeHTML(text).replace(/\n/g, '<br>');
            const avatar = isUser ? visitorAvatar() : agentAvatarURL();
            const bubbleBg = isUser ? 'bg-wc-user' : 'bg-wc-ai border border-gray-200/50';
            const bubbleDir = isUser ? 'bubble-right' : 'bubble-left';
            const textStyle = isUser ? 'text-wc-main' : 'text-wc-main';
            const wrapperMeta = { ...meta, retryText: meta.retryText || text };

            // B15：发送失败时展示重试图标。
            const failIcon = (isUser && isFailed) ? retryIconHTML(text) : '';

            // 客服/AI 头像旁显示名字（B12：使用真实客服名，缺失时回退默认文案）。
            const nameLabel = !isUser ? `<span class="text-[11px] text-gray-500 ml-1 mb-1 block">${escapeHTML(agentDisplayName())}</span>` : '';
            // AI 生成消息气泡下方的小标识
            const aiTagLabel = (!isUser && aiTag) ? `<span class="ai-tag text-[10px] text-gray-400 mt-1 ml-1 block select-none" dir="auto">${t('aiGenerated')}</span>` : '';

            return `
                <div class="flex gap-3 msg-wrapper ${isUser ? 'flex-row-reverse' : ''}" ${messageDataAttrs(wrapperMeta)}>
                    <img src="${escapeHTML(avatar)}" class="w-10 h-10 rounded shadow-sm shrink-0 object-cover" alt="Avatar">
                    <div class="flex flex-col max-w-[70%] ${isUser ? 'items-end' : 'items-start'}">
                        ${nameLabel}
                        <div class="flex items-center ${isUser ? 'flex-row-reverse' : 'flex-row'}">
                            <div class="${bubbleBg} ${textStyle} text-[16px] p-2.5 rounded-lg shadow-sm relative ${bubbleDir} leading-relaxed word-break-all min-h-[40px] flex items-center ${extraBubbleClass}">
                                ${safeText}
                            </div>
                            ${failIcon}
                        </div>
                        ${aiTagLabel}
                    </div>
                </div>
            `;
        }

        // 构建图片消息 (微信中的图片没有气泡尖角和背景)
        function createImageMessageHTML(imgSrc, isUser = false, meta = {}, aiTag = false, isFailed = false) {
            const avatar = isUser ? visitorAvatar() : agentAvatarURL();
            const nameLabel = !isUser ? `<span class="text-[11px] text-gray-500 ml-1 mb-1 block">${escapeHTML(agentDisplayName())}</span>` : '';
            const safeSrc = escapeHTML(safeImageSrc(imgSrc));
            const aiTagLabel = (!isUser && aiTag) ? `<span class="ai-tag text-[10px] text-gray-400 mt-1 ml-1 block select-none" dir="auto">${t('aiGenerated')}</span>` : '';
            const wrapperMeta = { ...meta, retryText: meta.retryText || imgSrc };
            const failIcon = (isUser && isFailed) ? retryIconHTML(imgSrc) : '';

            return `
                <div class="flex gap-3 msg-wrapper ${isUser ? 'flex-row-reverse' : ''}" ${messageDataAttrs(wrapperMeta)}>
                    <img src="${escapeHTML(avatar)}" class="w-10 h-10 rounded shadow-sm shrink-0 object-cover" alt="Avatar">
                    <div class="flex flex-col max-w-[70%] ${isUser ? 'items-end' : 'items-start'}">
                        ${nameLabel}
                        <div class="flex items-center ${isUser ? 'flex-row-reverse' : 'flex-row'}">
                            <div class="bubble-image mt-0.5">
                                <img src="${safeSrc}" alt="${t('image')}" class="border border-gray-200/50 shadow-sm">
                            </div>
                            ${failIcon}
                        </div>
                        ${aiTagLabel}
                    </div>
                </div>
            `;
        }

        function createAudioMessageHTML(audioSrc, isUser = false, meta = {}, aiTag = false, isFailed = false) {
            const avatar = isUser ? visitorAvatar() : agentAvatarURL();
            const nameLabel = !isUser ? `<span class="text-[11px] text-gray-500 ml-1 mb-1 block">${escapeHTML(agentDisplayName())}</span>` : '';
            const safeSrc = escapeHTML(resolveAssetURL(audioSrc));
            const bubbleBg = isUser ? 'bg-wc-user' : 'bg-wc-ai border border-gray-200/50';
            const bubbleDir = isUser ? 'bubble-right' : 'bubble-left';
            const aiTagLabel = (!isUser && aiTag) ? `<span class="ai-tag text-[10px] text-gray-400 mt-1 ml-1 block select-none" dir="auto">${t('aiGenerated')}</span>` : '';
            const wrapperMeta = { ...meta, retryText: meta.retryText || audioSrc };
            const failIcon = (isUser && isFailed) ? retryIconHTML(audioSrc) : '';

            return `
                <div class="flex gap-3 msg-wrapper ${isUser ? 'flex-row-reverse' : ''}" ${messageDataAttrs(wrapperMeta)}>
                    <img src="${escapeHTML(avatar)}" class="w-10 h-10 rounded shadow-sm shrink-0 object-cover" alt="Avatar">
                    <div class="flex flex-col max-w-[70%] ${isUser ? 'items-end' : 'items-start'}">
                        ${nameLabel}
                        <div class="flex items-center ${isUser ? 'flex-row-reverse' : 'flex-row'}">
                            <div class="${bubbleBg} bubble-audio rounded-2xl shadow-sm relative ${bubbleDir}">
                                <button type="button" class="voice-bubble-btn ${isUser ? 'voice-bubble-self' : 'voice-bubble-other'}" data-audio-bubble aria-label="${t('playVoiceMessage')}">
                                    <span class="voice-play-icon"><i class="fas fa-play" data-voice-icon></i></span>
                                    <span class="voice-wave" aria-hidden="true"><i></i><i></i><i></i></span>
                                    <span class="voice-duration" data-audio-duration>${t('voice')}</span>
                                </button>
                                <audio class="voice-audio-element" preload="metadata" src="${safeSrc}"></audio>
                            </div>
                            ${failIcon}
                        </div>
                        ${aiTagLabel}
                    </div>
                </div>
            `;
        }

        function createRevokedMessageHTML(text, isUser = false, meta = {}) {
            return createMessageHTML(text, isUser, false, meta, 'bubble-revoked italic');
        }

        function buildServerMessageHTML(msg, isUser = false) {
            const meta = {
                serverMsgId: msg.server_msg_id || '',
                clientMsgId: msg.client_msg_id || ''
            };
            const aiTag = !isUser && (msg.sender_type === 'ai' || msg.message_type === 'ai_text');
            // 初始消息（如欢迎语）服务端可能还带着译文，优先使用当前语言的译文展示。
            const translations = msg.translations || {};
            const lang = window.__CHAT_LANG__ || 'zh';
            if (!isUser && translations[lang] && String(translations[lang]).trim()) {
                msg = { ...msg, content: translations[lang] };
            }
            if (msg.message_type === 'image') {
                return createImageMessageHTML(resolveAssetURL(msg.content), isUser, meta, aiTag);
            }
            if (msg.message_type === 'audio') {
                return createAudioMessageHTML(resolveAssetURL(msg.content), isUser, meta, aiTag);
            }
            if (msg.message_type === 'revoked' || msg.revoked_at) {
                return createRevokedMessageHTML(msg.content || t('messageRecalled'), isUser, meta);
            }
            return createMessageHTML(msg.content || '', isUser, false, meta, '', aiTag);
        }

        function removeQuickReplyCards() {
            DOM.chatList.querySelectorAll('.quick-replies-card').forEach((node) => node.remove());
        }

        function bindQuickReplyActions(root) {
            root.querySelectorAll('[data-quick-reply]').forEach((button) => {
                button.addEventListener('click', () => {
                    const displayText = button.dataset.quickReply || '';
                    const sendText = button.dataset.sendText || displayText;
                    removeQuickReplyCards();
                    handleSend(sendText, displayText);
                });
            });
        }

        function renderQuickRepliesCard() {
            removeQuickReplyCards();
            if (!State.quickReplies.length) return;
            const items = State.quickReplies.map((item, index) => `
                <button
                    type="button"
                    class="w-full text-left text-sm text-[#576b95] active:bg-gray-100 px-1 py-1.5 rounded transition-colors"
                    data-quick-reply="${escapeHTML(item.text || '')}"
                    data-send-text="${escapeHTML(item.send_text || item.text || '')}"
                >${index + 1}. ${escapeHTML(item.text || '')}</button>
            `).join('');
            const html = `
                <div class="flex gap-3 msg-wrapper quick-replies-card">
                    <div class="w-10 shrink-0"></div>
                    <div class="bg-white p-3 rounded-lg shadow-sm border border-gray-200/50 w-[75%]">
                        <div class="text-sm text-gray-500 mb-2 border-b border-gray-100 pb-1">${t('suggestedQuestions')}</div>
                        <div class="space-y-1">${items}</div>
                    </div>
                </div>
            `;
            const div = document.createElement('div');
            div.innerHTML = html.trim();
            const el = div.firstElementChild;
            DOM.chatList.appendChild(el);
            bindQuickReplyActions(el);
            State.quickRepliesRendered = true;
            gsap.fromTo(el, { opacity: 0, y: 10 }, { opacity: 1, y: 0, duration: 0.2 });
            scrollToBottom();
        }

        function appendMessage(html, isUser = false, mode = 'append', options = {}) {
            const div = document.createElement('div');
            div.innerHTML = html.trim();
            const el = div.firstElementChild;
            if (mode === 'prepend') {
                DOM.historyAnchor.insertAdjacentElement('afterend', el);
            } else {
                DOM.chatList.appendChild(el);
            }
            el.querySelectorAll('.retry-icon').forEach(icon => {
                icon.addEventListener('click', () => retryMsg(icon, icon.dataset.retryText || ''));
            });
            bindAudioBubbleActions(el);
            
            // 微信没有浮夸的弹性动画，一般是直接出现或者极短的位移
            if (mode === 'append') {
                gsap.fromTo(el, { opacity: 0, y: 10 }, { opacity: 1, y: 0, duration: 0.2 });
            }
            
            if (mode === 'append') scrollToBottom();
            if (!isUser && !options.silent) playSound();
        }

        function replaceMessageElement(target, html) {
            if (!target) return;
            const div = document.createElement('div');
            div.innerHTML = html.trim();
            const next = div.firstElementChild;
            next.querySelectorAll('.retry-icon').forEach(icon => {
                icon.addEventListener('click', () => retryMsg(icon, icon.dataset.retryText || ''));
            });
            bindAudioBubbleActions(next);
            target.replaceWith(next);
        }

        function handleSend(manualText = null, displayText = null) {
            if (State.sessionType === 'ENDED') {
                showToast(t("sessionEndedCannotSend"));
                return;
            }
            
            const text = DOM.input.value.trim();
            const outgoingText = manualText || text;
            if (!outgoingText) return;
            const renderedText = displayText || outgoingText;

            // 清空输入框并重置状态
            DOM.input.value = '';
            DOM.input.style.height = '40px';
            DOM.btnPlus.style.display = 'flex';
            DOM.btnSend.style.display = 'none';
            closeAllPanels();
            removeQuickReplyCards();

            // B15：未连接时直接渲染失败气泡，点击重试图标可重发。
            if (!State.connected || !State.ws) {
                appendMessage(createMessageHTML(renderedText, true, true, {
                    msgType: 'text',
                    displayText: renderedText,
                    retryText: outgoingText
                }), true);
                showToast(t("messageNotSentReconnecting"));
                return;
            }
            const clientId = sendSocketMessage(outgoingText, 'text');
            appendMessage(createMessageHTML(renderedText, true, false, {
                clientMsgId: clientId,
                msgType: 'text',
                displayText: renderedText,
                retryText: outgoingText
            }), true);
        }

        // === B15：发送失败检测与重试 ===
        function trackPendingSend(clientMsgId) {
            if (!clientMsgId) return;
            clearPendingSend(clientMsgId);
            const timer = setTimeout(() => markMessageFailed(clientMsgId), 15000);
            State.pendingSends.set(clientMsgId, timer);
        }

        function clearPendingSend(clientMsgId) {
            const timer = State.pendingSends.get(clientMsgId);
            if (timer) clearTimeout(timer);
            State.pendingSends.delete(clientMsgId);
        }

        function failAllPendingSends() {
            [...State.pendingSends.keys()].forEach((id) => markMessageFailed(id));
        }

        // 把已渲染的本地消息就地替换成失败态（带重试图标）。
        function markMessageFailed(clientMsgId) {
            if (!clientMsgId) return;
            clearPendingSend(clientMsgId);
            const wrapper = DOM.chatList.querySelector(`.msg-wrapper[data-client-msg-id="${clientMsgId}"]`);
            if (!wrapper || wrapper.querySelector('.retry-icon')) return;
            const type = wrapper.dataset.msgType || 'text';
            const retryText = wrapper.dataset.retryText || '';
            const displayText = wrapper.dataset.displayText || retryText;
            const meta = { clientMsgId, msgType: type, displayText, retryText };
            let html;
            if (type === 'image') html = createImageMessageHTML(retryText, true, meta, false, true);
            else if (type === 'audio') html = createAudioMessageHTML(retryText, true, meta, false, true);
            else html = createMessageHTML(displayText, true, true, meta);
            replaceMessageElement(wrapper, html);
        }

        // 失败重试全局函数（B15）：点击重试图标重发同一条消息。
        window.retryMsg = function(icon, text) {
            const wrapper = icon.closest('.msg-wrapper');
            if (!wrapper) return;
            if (State.sessionType === 'ENDED') {
                showToast(t("sessionEndedCannotSend"));
                return;
            }
            if (!State.connected || !State.ws) {
                showToast(t("messageNotSentReconnecting"));
                return;
            }
            const type = wrapper.dataset.msgType || 'text';
            const displayText = wrapper.dataset.displayText || text;
            const retryText = wrapper.dataset.retryText || text;
            const newId = sendSocketMessage(retryText, type);
            if (!newId) return;
            const meta = { clientMsgId: newId, msgType: type, displayText, retryText };
            let html;
            if (type === 'image') html = createImageMessageHTML(retryText, true, meta);
            else if (type === 'audio') html = createAudioMessageHTML(retryText, true, meta);
            else html = createMessageHTML(displayText, true, false, meta);
            replaceMessageElement(wrapper, html);
            scrollToBottom();
        };

        async function uploadMedia(file, filename = '') {
            const form = new FormData();
            const uploadName = filename || file?.name || `upload-${Date.now()}`;
            form.append('file', file, uploadName);
            const resp = await fetch(`${State.apiBase}/api/uploads`, {
                method: 'POST',
                headers: { 'Authorization': `Bearer ${State.visitorToken}` },
                body: form
            });
            if (!resp.ok) {
                const detail = await resp.text().catch(() => '');
                throw new Error(`upload failed ${resp.status} ${detail}`.trim());
            }
            return resp.json();
        }

        function appendSystemMessage(text) {
            const html = `
                <div class="text-center mt-2 msg-wrapper">
                    <span class="text-[12px] text-gray-500 bg-[#dadada]/50 px-2 py-0.5 rounded-sm">${text}</span>
                </div>
            `;
            appendMessage(html, false);
        }

        // === "正在回复"输入指示 ===
        function showTypingIndicator(senderType) {
            hideTypingIndicator();
            State.typingSender = senderType === 'agent' ? 'agent' : 'ai';
            DOM.headerTyping.textContent = State.typingSender === 'agent' ? t('agentTyping') : t('aiTyping');
            DOM.headerTyping.classList.remove('hidden');
            // 提示展示期间隐藏标题与 AI 徽标，避免文字重叠。
            if (DOM.headerTitle && DOM.headerTitle.parentElement) {
                DOM.headerTitle.parentElement.style.visibility = 'hidden';
            }
            const avatar = State.typingSender === 'agent' ? State.agentAvatar : AVATAR_ASSETS.service;
            const html = `
                <div class="flex gap-3 msg-wrapper typing-indicator">
                    <img src="${escapeHTML(avatar)}" class="w-10 h-10 rounded shadow-sm shrink-0 object-cover" alt="Avatar">
                    <div class="flex flex-col max-w-[70%] items-start">
                        <span class="text-[11px] text-gray-500 ml-1 mb-1 block">${escapeHTML(State.agentName)}</span>
                        <div class="bg-wc-ai border border-gray-200/50 p-2.5 rounded-lg shadow-sm relative bubble-left min-h-[40px] flex items-center">
                            <span class="typing-dots" dir="ltr"><span></span><span></span><span></span></span>
                        </div>
                    </div>
                </div>
            `;
            appendMessage(html, false, 'append', { silent: true });
            // 兜底：AI 生成较慢给足时间；人工输入指示超时未续报则自动消失。
            State.typingTimer = setTimeout(() => hideTypingIndicator(), State.typingSender === 'agent' ? 8000 : 45000);
        }

        function hideTypingIndicator() {
            if (State.typingTimer) {
                clearTimeout(State.typingTimer);
                State.typingTimer = null;
            }
            State.typingSender = '';
            DOM.headerTyping.classList.add('hidden');
            if (DOM.headerTitle && DOM.headerTitle.parentElement) {
                DOM.headerTitle.parentElement.style.visibility = '';
            }
            DOM.chatList.querySelectorAll('.typing-indicator').forEach((node) => node.remove());
        }

        // === 状态机与业务动作 ===

        function updateSessionState(newState) {
            // 无痕转人工：排队等待人工期间不存在"转接中"状态，访客侧保持 AI 接待。
            if (newState === 'QUEUING') newState = 'AI';
            State.sessionType = newState;
            
            // 基础重置
            resetSystemBanner();
            DOM.headerAiBadge.classList.add('hidden');
            DOM.input.disabled = false;
            DOM.input.placeholder = "";

            switch (newState) {
                case 'AI':
                    State.agentName = t('agentNameDefault');
                    State.agentAvatar = AVATAR_ASSETS.service;
                    State.humanRequested = false;
                    DOM.headerAiBadge.classList.remove('hidden');
                    break;
                case 'HUMAN':
                    // B12：使用会话下发的真实客服名/头像，缺失时才回退默认文案。
                    State.agentName = agentDisplayName();
                    State.agentAvatar = agentAvatarURL();
                    State.humanRequested = false;
                    DOM.sysBanner.classList.remove('hidden');
                    DOM.sysBanner.className = 'bg-[#f0f9eb] text-[#67c23a] text-xs px-3 py-2 flex items-center justify-center border-b border-[#e1f3d8] shrink-0 z-10 shadow-sm relative';
                    DOM.sysBannerText.innerHTML = '<i class="fas fa-check-circle mr-1"></i>' + t('humanConnected');
                    setTimeout(() => DOM.sysBanner.classList.add('hidden'), 3000);
                    break;
                case 'ENDED':
                    State.agentName = t('sessionEnded');
                    DOM.input.disabled = true;
                    DOM.input.placeholder = t("sessionEnded");
                    break;
            }
            DOM.headerTitle.textContent = State.agentName;
        }

        // === B14：恢复历史会话时发现会话已结束 ===
        // 保留已渲染的历史消息，给出明确的"会话已结束"状态与评价入口，
        // 而不是直接把访客踢回入口页。
        function enterEndedSessionState(options = {}) {
            State.keepHistoryOnEnd = options.keepHistory !== false;
            State.connected = false;
            State.pendingReturnToEntry = false;
            disconnectSocket();
            updateSessionState('ENDED');
            showEndedBanner();
            const shouldPrompt = options.promptRating !== false
                && !State.ratingSubmitted
                && !!State.visitorToken
                && !!State.conversationId;
            if (shouldPrompt) {
                State.evalReturnMessage = '';
                showEvalSheet();
            }
        }

        function showEndedBanner() {
            DOM.sysBanner.classList.remove('hidden');
            DOM.sysBanner.className = 'bg-[#f4f4f5] text-[#6b7280] text-xs px-3 py-2 flex items-center justify-center border-b border-[#e5e7eb] shrink-0 z-10 shadow-sm relative';
            DOM.sysBannerText.innerHTML = '<i class="fas fa-info-circle mr-1"></i>'
                + escapeHTML(t('sessionEndedKeepHistory'))
                + ' <a href="#" class="text-blue-500 underline ml-1" data-action="restart">'
                + escapeHTML(t('restartConsultation'))
                + '</a>';
        }

        // 已结束会话中的"重新咨询"入口。
        window.restartConsultation = async function() {
            disconnectSocket();
            clearVisitorSession();
            resetConversationRuntime();
            resetChatView();
            hideEvalImmediately();
            updateSessionState('AI');
            State.startingConversation = false;
            DOM.entryBtn.disabled = false;
            DOM.entryBtn.textContent = t('consultService');
            if (isEmbedMode()) {
                await openConsultation();
                return;
            }
            showEntryView();
        };

        function triggerAction(action) {
            closeAllPanels();
            
            if (action === 'photo') {
                if (State.sessionType === 'ENDED') {
                    showToast(t("sessionEnded"));
                    return;
                }
                DOM.uploadImageInput.click();
            }
            else if (action === 'human') {
                // B11：转人工入口（此前 triggerAction('human') 是死代码）。
                if (State.sessionType === 'HUMAN') {
                    showToast(t("alreadyHumanState"));
                    return;
                }
                if (State.sessionType === 'ENDED') {
                    showToast(t("sessionEnded"));
                    return;
                }
                if (!State.connected || !State.ws) {
                    showToast(t("messageNotSentReconnecting"));
                    return;
                }
                // 无痕转人工：后台静默排队分配客服，AI 继续接待；
                // 这里额外给出"等待人工接入"的可见提示，保证 UI 状态合理。
                const handoffId = sendSocketMessage('', 'handoff_request');
                // 让服务端回执的本地化请求文案作为访客自己的气泡渲染出来。
                if (handoffId) State.renderedClientMsgIds.delete(handoffId);
                State.humanRequested = true;
                appendSystemMessage(t('humanRequested'));
                showToast(t('humanRequested'));
            } 
            else if (action === 'phone') {
				const telephone = State.contacts.telephone;
                appendSystemMessage(t('supportPhonePrefix') + `<a href="tel:${escapeHTML(telephone)}" class="text-blue-500">${escapeHTML(telephone)}</a> ` + t('tapToDial'));
                if (State.connected) sendSocketMessage("电话");
            }
            else if (action === 'whatsapp') {
                const whatsapp = State.contacts.whatsapp;
                appendSystemMessage(t('officialWhatsappPrefix') + `<span class="text-wc-user font-bold select-all">${escapeHTML(whatsapp)}</span> ` + t('longPressCopy'));
                if (State.connected) sendSocketMessage("WhatsApp");
            }
            else if (action === 'telegram') {
                const telegram = State.contacts.telegram;
                appendSystemMessage(t('officialTelegramPrefix') + `<span class="text-wc-user font-bold select-all">${escapeHTML(telegram)}</span> ` + t('longPressCopy'));
                if (State.connected) sendSocketMessage("Telegram");
            }
            else if (action === 'end') {
                if (State.sessionType === 'ENDED' || State.pendingReturnToEntry) return;
                if (State.connected && State.ws && State.ws.readyState === WebSocket.OPEN) {
                    State.pendingReturnToEntry = true;
                    updateSessionState('ENDED');
                    setTimeout(() => {
                        if (State.pendingReturnToEntry) {
                            promptForRatingOrReturn(t("sessionEnded"));
                        }
                    }, 600);
                    try {
                        State.ws.send(JSON.stringify({
                            event: 'conversation.close',
                            data: { conversation_id: State.conversationId }
                        }));
                    } catch (err) {
                        // WebSocket 可能刚好断开；仍然继续评价流程，不让 send() 中断兜底。
                    }
                } else {
                    promptForRatingOrReturn(t("sessionEnded"));
                }
            }
        }

        async function initConversation() {
            try {
                await loadContactSettings();
                const payload = {
                    source: window.innerWidth >= 768 ? 'web_pc' : 'web_mobile',
                    lang: window.__CHAT_LANG__ || 'zh'
                };
                // 画像聚合键：只有官网埋点已写入访客 ID（且通过校验）时才带上该字段。
                const visitorRef = readVisitorRef();
                if (visitorRef) payload.visitor_ref = visitorRef;
                const resp = await fetch(`${State.apiBase}/api/visitor/conversations`, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(payload)
                });
                if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
                const data = await resp.json();
                State.visitorToken = data.token;
                State.conversationId = data.conversation.id;
                State.pendingInitialMessages = data.initial_messages || [];
                State.nextBefore = data.initial_messages?.[0]?.server_msg_id || '';
                applyConversationState(data.conversation);
                connectSocket();
                return true;
            } catch (err) {
                console.error(err);
                showToast(t("connectSystemFailed"), 3000);
                return false;
            }
        }

        async function loadContactSettings() {
            try {
                const resp = await fetch(`${State.apiBase}/api/contact-settings`);
                if (!resp.ok) return;
                const data = await resp.json();
                State.contacts = {
                    ...State.contacts,
                    telephone: data.telephone || State.contacts.telephone,
                    whatsapp: data.whatsapp || State.contacts.whatsapp,
                    telegram: data.telegram || State.contacts.telegram
                };
                State.quickReplies = Array.isArray(data.quick_replies) ? data.quick_replies : [];
            } catch (err) {}
        }

        // 把当前网站语言同步到会话（访客切换语言后，AI 回复与翻译按新语言处理）。
        // 语言未变时后端直接返回，无副作用；失败不影响正常聊天。
        // B14：返回会话对象（含 status 与 assigned_agent_*），供恢复历史时判断状态。
        async function syncVisitorLangToServer() {
            if (!State.visitorToken || !State.conversationId) return null;
            const lang = window.__CHAT_LANG__ || 'zh';
            try {
                const resp = await fetch(`${State.apiBase}/api/visitor/lang`, {
                    method: 'POST',
                    headers: {
                        'Content-Type': 'application/json',
                        'Authorization': `Bearer ${State.visitorToken}`
                    },
                    body: JSON.stringify({ lang })
                });
                if (!resp.ok) return null;
                return await resp.json();
            } catch (err) {
                return null;
            }
        }

        async function drainPendingInitialMessages() {
            const pending = Array.isArray(State.pendingInitialMessages) ? [...State.pendingInitialMessages] : [];
            State.pendingInitialMessages = [];
            for (const msg of pending) {
                // 非中文访客的 AI 欢迎语等初始消息若还没有当前语言译文，先不渲染，
                // 等翻译完成后的推送/历史同步补上，避免"先闪一下中文"。
                if (initialMessageAwaitingTranslation(msg)) {
                    scheduleInitialMessageFallback(msg);
                    continue;
                }
                if (msg.server_msg_id) State.renderedServerMsgIds.add(msg.server_msg_id);
                if (msg.client_msg_id) State.renderedClientMsgIds.add(msg.client_msg_id);
                renderServerMessage(msg, 'append', { force: true, silent: true });
                await new Promise((resolve) => setTimeout(resolve, 220));
            }
            if (!State.quickRepliesRendered) {
                renderQuickRepliesCard();
            }
        }

        function initialMessageAwaitingTranslation(msg) {
            const lang = window.__CHAT_LANG__ || 'zh';
            if (lang === 'zh' || !msg) return false;
            if (msg.sender_type !== 'ai' || msg.message_type !== 'ai_text') return false;
            const translated = msg.translations && msg.translations[lang];
            return !String(translated || '').trim();
        }

        function scheduleInitialMessageFallback(msg) {
            // 翻译迟迟不到（或失败）时的兜底：6 秒后仍无译文就显示原文，保证欢迎语不丢。
            setTimeout(() => {
                if (!msg.server_msg_id || State.renderedServerMsgIds.has(msg.server_msg_id)) return;
                renderServerMessage(msg, 'append', { force: true, silent: true });
            }, 6000);
        }

        function connectSocket() {
            if (!State.visitorToken || !State.conversationId) return;
            const url = `${inferWSBase()}/ws?role=visitor&token=${encodeURIComponent(State.visitorToken)}&conversation_id=${encodeURIComponent(State.conversationId)}`;
            State.ws = new WebSocket(url);
            State.ws.onopen = () => {
                State.connected = true;
                showToast(t("channelConnected"));
                drainPendingInitialMessages()
                    .catch(() => {})
                    .finally(() => {
                        syncMessages().catch(() => {});
                    });
            };
            State.ws.onclose = () => {
                State.connected = false;
                // B15：断线时把待回执的消息标记为失败，允许手动重发。
                failAllPendingSends();
                if (!State.entryVisible && State.conversationId && State.sessionType !== 'ENDED') {
                    setTimeout(connectSocket, 2000);
                }
            };
            State.ws.onerror = () => {
                State.connected = false;
            };
            State.ws.onmessage = (event) => {
                const payload = JSON.parse(event.data);
                handleSocketEvent(payload);
            };
        }

        function sendSocketMessage(text, type = 'text') {
            if (!State.connected || !State.ws) {
                return '';
            }
            const id = clientMsgId();
            State.renderedClientMsgIds.add(id);
            trackPendingSend(id);
            State.ws.send(JSON.stringify({
                event: 'message.send',
                data: {
                    conversation_id: State.conversationId,
                    client_msg_id: id,
                    message_type: type,
                    content: text
                }
            }));
            return id;
        }

        function handleSocketEvent(payload) {
            if (payload.event === 'message.ack') {
                // 回执按 client_msg_id 去重：本地已渲染的不会重复出现；
                // 人工客服请求等由服务端补齐文案的消息会在此渲染为访客气泡。
                clearPendingSend(payload.data?.client_msg_id || '');
                renderServerMessage(payload.data, 'append');
                return;
            }
            if (payload.event === 'message.receive') {
                const msg = payload.data;
                // AI/客服的消息到达即结束"正在回复"指示。
                if (msg && (msg.sender_type === 'ai' || msg.sender_type === 'agent')) hideTypingIndicator();
                renderServerMessage(msg, 'append');
                if (msg && msg.sender_type !== 'visitor') {
                    notifyHost('servicebridge:unread', { delta: 1 });
                }
                return;
            }
            if (payload.event === 'conversation.typing') {
                const data = payload.data || {};
                if (data.typing) showTypingIndicator(data.sender_type === 'agent' ? 'agent' : 'ai');
                else hideTypingIndicator();
                return;
            }
            if (payload.event === 'message.revoked') {
                const msg = payload.data;
                renderServerMessage(msg, 'append', { force: true, silent: true, replaceOnly: true });
                return;
            }
            if (payload.event === 'conversation.status_changed' || payload.event === 'conversation.assigned') {
                applyConversationState(payload.data);
                return;
            }
            if (payload.event === 'error') {
                const code = payload.data?.code || '';
                if (code === 'unauthorized') {
                    returnToEntry(t("sessionExpired"));
                    return;
                }
                if (code === 'forbidden' || code === 'not_found') {
                    if (State.pendingReturnToEntry) {
                        promptForRatingOrReturn(t("sessionEnded"));
                        return;
                    }
                    // B14：会话已结束/已失效时保留历史，给出"会话已结束"与评价入口。
                    if (State.keepHistoryOnEnd || State.sessionType === 'ENDED') {
                        enterEndedSessionState({ keepHistory: true, promptRating: false });
                        return;
                    }
                    returnToEntry(t("sessionExpired"));
                    return;
                }
                // 普通错误（如会话已关闭导致发送被拒）：把待回执的消息标记为失败可重试。
                failAllPendingSends();
                showToast(payload.data?.message || t("channelError"));
            }
        }

        function renderServerMessage(msg, mode = 'append', options = {}) {
            if (!msg) return;
            // 过滤仅客服端可见的隐藏消息（如转接提示）与空消息。
            if (msg.hidden_from_visitor) return;
            if (!msg.sender_type && !String(msg.content || '').trim()) return;
            const target = findMessageElement({
                serverMsgId: msg.server_msg_id || '',
                clientMsgId: msg.client_msg_id || ''
            });
            const isUser = msg.sender_type === 'visitor';
            const html = buildServerMessageHTML(msg, isUser);
            if (target) {
                replaceMessageElement(target, html);
                return;
            }
            if (options.replaceOnly) return;
            if (!options.force && msg.server_msg_id && State.renderedServerMsgIds.has(msg.server_msg_id)) return;
            if (!options.force && msg.client_msg_id && State.renderedClientMsgIds.has(msg.client_msg_id)) return;
            if (msg.server_msg_id) State.renderedServerMsgIds.add(msg.server_msg_id);
            if (msg.client_msg_id) State.renderedClientMsgIds.add(msg.client_msg_id);
            appendMessage(html, isUser, mode, options);
        }

        async function syncMessages(mode = 'append') {
            if (!State.conversationId || !State.visitorToken) return false;
            const resp = await fetch(`${State.apiBase}/api/conversations/${encodeURIComponent(State.conversationId)}/messages?limit=50`, {
                headers: { 'Authorization': `Bearer ${State.visitorToken}` }
            });
            if (resp.status === 401 || resp.status === 403 || resp.status === 404) return false;
            if (!resp.ok) throw new Error(`sync failed ${resp.status}`);
            const data = await resp.json();
            State.hasMoreHistory = !!data.has_more;
            State.nextBefore = data.next_before || (data.messages?.[0]?.server_msg_id || State.nextBefore);
            if (DOM.loadHistory) DOM.loadHistory.textContent = State.hasMoreHistory ? t('loadMoreHistory') : t('noMoreHistory');
            (data.messages || []).forEach((msg) => renderServerMessage(msg, mode));
            return true;
        }

        async function loadOlderMessages() {
            if (State.loadingHistory) return;
            if (!State.hasMoreHistory || !State.nextBefore) {
                showToast(t("noMoreHistory"));
                return;
            }
            State.loadingHistory = true;
            try {
                const resp = await fetch(`${State.apiBase}/api/conversations/${encodeURIComponent(State.conversationId)}/messages?limit=30&before=${encodeURIComponent(State.nextBefore)}`, {
                    headers: { 'Authorization': `Bearer ${State.visitorToken}` }
                });
                if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
                const data = await resp.json();
                State.hasMoreHistory = !!data.has_more;
                State.nextBefore = data.next_before || (data.messages?.[0]?.server_msg_id || State.nextBefore);
                [...(data.messages || [])].reverse().forEach((msg) => renderServerMessage(msg, 'prepend'));
                DOM.loadHistory.textContent = State.hasMoreHistory ? t('loadMoreHistory') : t('noMoreHistory');
            } catch (err) {
                showToast(t("historyLoadFailed"));
            } finally {
                State.loadingHistory = false;
            }
        }

        function applyConversationState(conversation) {
            if (!conversation) return;
            // B12：先记录真实客服信息，再切换状态，保证 HUMAN 分支能取到。
            applyAssignedAgent(conversation);
            if (conversation.status === 'assigned') {
                updateSessionState('HUMAN');
            } else if (conversation.status === 'ai_serving') {
                updateSessionState('AI');
            } else if (conversation.status === 'waiting' || conversation.status === 'human_requested') {
                // 无痕转人工：排队等待人工期间访客侧仍显示 AI 接待，不展示转接提示。
                updateSessionState('AI');
            } else if (conversation.status === 'closed') {
                if (State.pendingReturnToEntry) {
                    // 主动结束会话时本地会先进入 ENDED；该状态仍应完成本次评价流程。
                    promptForRatingOrReturn(t("sessionEnded"));
                    return;
                }
                if (State.keepHistoryOnEnd || State.sessionType === 'ENDED') {
                    // B14：已进入"结束但保留历史"模式，维持现状。
                    enterEndedSessionState({ keepHistory: true, promptRating: false });
                    return;
                }
                promptForRatingOrReturn(t("sessionEnded"));
                return;
            }
            saveVisitorSession();
        }

        // === 评价组件逻辑 ===
        const RATING_ACTIVE_COLORS = {
            satisfied: '#07c160',
            neutral: '#f5a623',
            unhappy: '#ef4444'
        };

        function ratingColor(score) {
            const value = Number(score || 0);
            if (value >= 5) return RATING_ACTIVE_COLORS.satisfied;
            if (value >= 3) return RATING_ACTIVE_COLORS.neutral;
            return RATING_ACTIVE_COLORS.unhappy;
        }

        function resetRatingIcons() {
            document.querySelectorAll('.eval-star i').forEach(i => {
                i.style.color = '';
                i.classList.add('text-gray-300');
            });
        }

        function showEvalSheet() {
            if (State.ratingPromptVisible) return;
            State.ratingPromptVisible = true;
            DOM.evalOverlay.classList.remove('hidden');
            DOM.evalOverlay.classList.add('flex');
            gsap.to(DOM.evalSheet, { y: 0, duration: 0.3, ease: "power3.out" });
            
            // 重置星星
            resetRatingIcons();
            State.selectedRatingScore = 0;
            DOM.evalAdvice.value = '';
            syncEvalAdviceBox();
        }

        function closeEval(closeHostAfter = false) {
            gsap.to(DOM.evalSheet, { y: '100%', duration: 0.2, ease: "power2.in", onComplete: () => {
                DOM.evalOverlay.classList.remove('flex');
                DOM.evalOverlay.classList.add('hidden');
                State.ratingPromptVisible = false;
                // 评价成功后都结束当前访客会话。清理 iframe 和主站的一方会话副本，
                // 再通知主站收起浮窗，避免恢复的已结束会话仍卡在历史页。
                if (closeHostAfter) {
                    const conversationId = State.conversationId;
                    clearVisitorSession();
                    notifyHost('servicebridge:conversation-closed', {
                        conversationId
                    });
                }
                if (State.evalReturnMessage) {
                    const message = State.evalReturnMessage;
                    State.evalReturnMessage = '';
                    returnToEntry(message);
                } else if (closeHostAfter) {
                    returnToEntry();
                }
            }});
        }

        // 星星点击事件
        document.querySelectorAll('.eval-star').forEach(star => {
            star.addEventListener('click', function() {
                // 清除所有颜色
                resetRatingIcons();
                // 点亮当前
                const score = Number(this.dataset.score || 0);
                const icon = this.querySelector('i');
                icon.classList.remove('text-gray-300');
                icon.style.color = ratingColor(score);
                State.selectedRatingScore = score;
                syncEvalAdviceBox();
                // 满意评价无需再点一次提交：立即保存，成功后关闭评价面板。
                if (score >= 5) submitRating(score, '').catch(() => {});
            });
        });

        function syncEvalAdviceBox() {
            const needsAdvice = State.selectedRatingScore > 0 && State.selectedRatingScore < 5;
            DOM.evalAdviceBox.classList.toggle('hidden', !needsAdvice);
            if (!needsAdvice) DOM.evalAdvice.value = '';
        }

        async function submitRating(score, comment) {
            if (State.ratingSubmitted) {
                closeEval(true);
                return true;
            }
            if (State.ratingSubmitting) return false;
            State.ratingSubmitting = true;
            document.querySelectorAll('.eval-star').forEach((star) => {
                star.classList.add('pointer-events-none', 'opacity-60');
            });
            const submitButton = DOM.evalOverlay.querySelector('[data-action="submit-eval"]');
            if (submitButton) submitButton.disabled = true;
            try {
                const resp = await fetch(`${State.apiBase}/api/visitor/conversations/${encodeURIComponent(State.conversationId)}/rating`, {
                    method: 'POST',
                    headers: {
                        'Content-Type': 'application/json',
                        'Authorization': `Bearer ${State.visitorToken}`
                    },
                    body: JSON.stringify({
                        score,
                        tags: ratingTags(score),
                        comment
                    })
                });
                if (!resp.ok && resp.status !== 409) throw new Error('rating failed');
                State.ratingSubmitted = true;
                showToast(t("thanksRating"));
                closeEval(true);
                return true;
            } catch (err) {
                showToast(t("ratingSubmitFailed"));
                return false;
            } finally {
                State.ratingSubmitting = false;
                document.querySelectorAll('.eval-star').forEach((star) => {
                    star.classList.remove('pointer-events-none', 'opacity-60');
                });
                if (submitButton) submitButton.disabled = false;
            }
        }

        window.submitEval = async function() {
            if(!State.selectedRatingScore) {
                showToast(t("chooseRatingFirst"));
                return;
            }
            const advice = DOM.evalAdvice.value.trim();
            if (State.selectedRatingScore < 5 && !advice) {
                showToast(t("enterSuggestion"));
                DOM.evalAdvice.focus();
                return;
            }
            await submitRating(State.selectedRatingScore, advice);
        }

        function ratingTags(score) {
            if (score >= 5) return [t('verySatisfied')];
            if (score >= 3) return [t('neutral')];
            return [t('unsatisfied')];
        }

        // 顶部按钮事件
        DOM.btnSound.addEventListener('click', function() {
            State.soundEnabled = !State.soundEnabled;
            if (State.soundEnabled) {
                this.className = 'fas fa-volume-up text-lg text-gray-700 cursor-pointer';
                showToast(t("soundOn"));
            } else {
                this.className = 'fas fa-volume-mute text-lg text-gray-400 cursor-pointer';
                showToast(t("soundOff"));
            }
        });

        DOM.endConfirmOverlay.addEventListener('click', (event) => {
            if (event.target === DOM.endConfirmOverlay) {
                closeEndConfirm();
            }
        });

        DOM.endConfirmCancel.addEventListener('click', () => {
            closeEndConfirm();
        });

        DOM.endConfirmSubmit.addEventListener('click', () => {
            confirmEndConversation();
        });

        DOM.uploadImageInput.addEventListener('change', async function() {
            const file = this.files && this.files[0];
            this.value = '';
            if (!file) return;
            if (!State.visitorToken) {
                showToast(t("channelNotInitialized"));
                return;
            }
            try {
                showToast(t("imageUploading"));
                const data = await uploadMedia(file, file.name || `image-${Date.now()}.png`);
                const imageURL = resolveAssetURL(data.url);
                // B15：连接断开时以失败态渲染，点击图标可重发。
                const clientId = State.connected && State.ws ? sendSocketMessage(data.url, 'image') : '';
                appendMessage(createImageMessageHTML(imageURL, true, {
                    clientMsgId: clientId,
                    msgType: 'image',
                    displayText: data.url,
                    retryText: data.url
                }, false, !clientId), true);
                if (!clientId) showToast(t("messageNotSentReconnecting"));
            } catch (err) {
                console.error(err);
                showToast(t("imageUploadFailed"));
            }
        });

        function preferredAudioMimeType() {
            if (typeof MediaRecorder === 'undefined' || typeof MediaRecorder.isTypeSupported !== 'function') return '';
            const candidates = [
                'audio/webm;codecs=opus',
                'audio/webm',
                'audio/mp4',
                'audio/ogg;codecs=opus'
            ];
            return candidates.find((item) => MediaRecorder.isTypeSupported(item)) || '';
        }

        function audioExtension(mimeType) {
            if (String(mimeType).includes('ogg')) return '.ogg';
            if (String(mimeType).includes('mp4')) return '.m4a';
            if (String(mimeType).includes('mpeg') || String(mimeType).includes('mp3')) return '.mp3';
            return '.webm';
        }

        async function startVoiceRecording(startY = 0) {
            if (State.isRecording || State.sessionType === 'ENDED') return;
            if (!navigator.mediaDevices || typeof MediaRecorder === 'undefined') {
                showToast(t('recordingNotSupported'));
                State.recordGestureSource = '';
                State.recordPointerId = null;
                return;
            }
            try {
                closeAllPanels();
                const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
                const mimeType = preferredAudioMimeType();
                const recorder = mimeType ? new MediaRecorder(stream, { mimeType }) : new MediaRecorder(stream);
                State.recordingStream = stream;
                State.mediaRecorder = recorder;
                State.recordingChunks = [];
                State.recordingCanceled = false;
                State.isRecording = true;
                State.recordingStartY = startY;
                State.recordingStartedAt = Date.now();
                updateRecordingOverlay(false);
                recorder.ondataavailable = (event) => {
                    if (event.data && event.data.size > 0) {
                        State.recordingChunks.push(event.data);
                    }
                };
                recorder.onstop = async () => {
                    const chunks = State.recordingChunks.slice();
                    const canceled = State.recordingCanceled;
                    const startedAt = State.recordingStartedAt;
                    const type = recorder.mimeType || mimeType || 'audio/webm';
                    State.mediaRecorder = null;
                    State.recordingChunks = [];
                    State.isRecording = false;
                    State.recordingCanceled = false;
                    State.recordingStartedAt = 0;
                    State.recordingStartY = 0;
                    State.recordGestureSource = '';
                    State.recordPointerId = null;
                    State.recordPendingStop = false;
                    State.recordPendingForceCancel = false;
                    stopRecordingStream();
                    hideRecordingOverlay();
                    if (canceled) {
                        showToast(t('sendingCancelled'));
                        return;
                    }
                    if (!chunks.length || Date.now() - startedAt < 400) {
                        showToast(t('recordingTooShort'));
                        return;
                    }
                    try {
                        showToast(t('voiceUploading'));
                        const blob = new Blob(chunks, { type });
                        const fileName = `voice-${Date.now()}${audioExtension(type)}`;
                        let payload = blob;
                        if (typeof File === 'function') {
                            payload = new File([blob], fileName, { type, lastModified: Date.now() });
                        }
                        const data = await uploadMedia(payload, fileName);
                        const audioURL = resolveAssetURL(data.url);
                        // B15：连接断开时以失败态渲染，点击图标可重发。
                        const clientId = State.connected && State.ws ? sendSocketMessage(data.url, 'audio') : '';
                        appendMessage(createAudioMessageHTML(audioURL, true, {
                            clientMsgId: clientId,
                            msgType: 'audio',
                            displayText: data.url,
                            retryText: data.url
                        }, false, !clientId), true);
                        if (!clientId) showToast(t('messageNotSentReconnecting'));
                    } catch (err) {
                        console.error(err);
                        showToast(t('voiceSendFailed'));
                    }
                };
                recorder.start();
                if (State.recordPendingStop) {
                    finishVoiceRecording(State.recordPendingForceCancel);
                }
            } catch (err) {
                console.error(err);
                State.recordGestureSource = '';
                State.recordPointerId = null;
                State.recordPendingStop = false;
                State.recordPendingForceCancel = false;
                stopRecordingStream();
                hideRecordingOverlay();
                showToast(t('micPermissionDenied'));
            }
        }

        function updateVoiceRecordingPosition(clientY, clientX = 0) {
            if (!State.recordGestureSource || clientY <= 0 || State.recordingStartY <= 0) return;
            const deltaY = State.recordingStartY - clientY;
            const enterThreshold = 96;
            const leaveThreshold = 56;
            const overCancelZone = isWithinCancelZone(clientX, clientY);
            let shouldCancel = State.recordingCanceled;
            if (shouldCancel) {
                shouldCancel = overCancelZone || deltaY >= leaveThreshold;
            } else {
                shouldCancel = overCancelZone || deltaY >= enterThreshold;
            }
            State.recordingCanceled = shouldCancel;
            State.recordPendingForceCancel = shouldCancel;
            if (State.isRecording) {
                updateRecordingOverlay(shouldCancel);
            }
        }

        function finishVoiceRecording(forceCancel = false) {
            if (!State.recordGestureSource && !State.isRecording) return;
            State.recordPendingStop = true;
            State.recordPendingForceCancel = State.recordPendingForceCancel || State.recordingCanceled || forceCancel;
            if (!State.isRecording || !State.mediaRecorder) return;
            State.recordingCanceled = State.recordPendingForceCancel;
            if (State.mediaRecorder.state !== 'inactive') {
                State.mediaRecorder.stop();
            }
        }

        DOM.btnVoice.addEventListener('pointerdown', (event) => {
            if (event.pointerType === 'mouse' && event.button !== 0) return;
            beginVoiceGesture(event, 'pointer');
        });

        window.addEventListener('pointermove', (event) => {
            if (State.recordGestureSource !== 'pointer') return;
            if (State.recordPointerId != null && event.pointerId != null && event.pointerId !== State.recordPointerId) return;
            updateVoiceRecordingPosition(event.clientY || 0, event.clientX || 0);
        });

        window.addEventListener('pointerup', (event) => {
            if (State.recordGestureSource !== 'pointer') return;
            if (State.recordPointerId != null && event.pointerId != null && event.pointerId !== State.recordPointerId) return;
            finishVoiceRecording(false);
        });

        window.addEventListener('pointercancel', (event) => {
            if (State.recordGestureSource !== 'pointer') return;
            if (State.recordPointerId != null && event.pointerId != null && event.pointerId !== State.recordPointerId) return;
            finishVoiceRecording(true);
        });

        DOM.btnVoice.addEventListener('touchstart', (event) => {
            beginVoiceGesture(event, 'touch');
        }, { passive: false });

        window.addEventListener('touchmove', (event) => {
            if (State.recordGestureSource !== 'touch') return;
            event.preventDefault();
            const point = voicePoint(event);
            updateVoiceRecordingPosition(point.y || 0, point.x || 0);
        }, { passive: false });

        window.addEventListener('touchend', (event) => {
            if (State.recordGestureSource !== 'touch') return;
            event.preventDefault();
            finishVoiceRecording(false);
        }, { passive: false });

        window.addEventListener('touchcancel', (event) => {
            if (State.recordGestureSource !== 'touch') return;
            event.preventDefault();
            finishVoiceRecording(true);
        }, { passive: false });

        // 右上角 "..." 结束会话
        document.getElementById('btn-more-top').addEventListener('click', () => {
            if(State.sessionType !== 'ENDED') {
                showEndConfirm();
            }
        });

        // 阻止输入框回车换行，直接发送
        DOM.input.addEventListener('keypress', function(e) {
            if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault();
                handleSend();
            }
        });

        // 启动初始化
        window.addEventListener('DOMContentLoaded', async () => {
            notifyHost('servicebridge:ready');
            initEmojis();
            hideEvalImmediately();
            hideEndConfirmImmediately();
            await bootApp();
        });



// 严格 CSP 下不允许内联 onclick；统一委托到 data-action。
document.addEventListener('click', (event) => {
  const element = event.target instanceof Element ? event.target.closest('[data-action]') : null;
  if (!element) return;
  const action = element.getAttribute('data-action');
  const arg = element.getAttribute('data-arg') || '';
  switch (action) {
    case 'open-consultation': openConsultation(); break;
    case 'dismiss-parent': element.parentElement?.classList.add('hidden'); break;
    case 'close-panels': closeAllPanels(); break;
    case 'load-older': loadOlderMessages(); break;
    case 'toggle-panel': togglePanel(arg); break;
    case 'send': handleSend(); break;
    case 'trigger': triggerAction(arg); break;
    case 'close-eval': closeEval(); break;
    case 'submit-eval': window.submitEval?.(); break;
    case 'restart':
      event.preventDefault();
      window.restartConsultation?.();
      break;
    default: break;
  }
});
