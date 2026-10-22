/**
 * WSBroker Display Controller - Modern Dashboard Interactive Client
 * - Server-Sent Events (SSE) stream listener with automatic reconnection
 * - Toast notification manager
 * - Real-time synchronized clock with NTP offset
 * - Client-side instant display search and status filtering
 * - Live stream pause / resume controls
 */

(function() {
    'use strict';

    let ntpOffsetNanos = 0;
    let eventSource = null;
    let reconnectTimeout = null;
    let reconnectAttempts = 0;
    let isStreamPaused = false;
    let refreshDebounceTimer = null;

    // --- Toast Notification System ---
    window.showToast = function(level, title, message, duration = 4500) {
        const container = document.getElementById('toast-container');
        if (!container) return;

        level = (level || 'info').toLowerCase();
        const toast = document.createElement('div');
        toast.className = `toast-item toast-${level} p-3.5 mb-2.5 flex items-start space-x-3 text-sm text-slate-100`;

        let iconSvg = '';
        if (level === 'success') {
            iconSvg = `<svg class="w-5 h-5 text-emerald-400 shrink-0 mt-0.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7"></path></svg>`;
        } else if (level === 'error') {
            iconSvg = `<svg class="w-5 h-5 text-rose-400 shrink-0 mt-0.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"></path></svg>`;
        } else {
            iconSvg = `<svg class="w-5 h-5 text-indigo-400 shrink-0 mt-0.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"></path></svg>`;
        }

        toast.innerHTML = `
            ${iconSvg}
            <div class="flex-1 pr-1 overflow-hidden">
                ${title ? `<div class="font-semibold text-xs tracking-wide uppercase text-slate-200">${escapeHtml(title)}</div>` : ''}
                <div class="text-xs text-slate-300 break-words mt-0.5">${escapeHtml(message)}</div>
            </div>
            <button class="text-slate-400 hover:text-white transition p-1 shrink-0" aria-label="Close">
                <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"></path></svg>
            </button>
        `;

        const closeBtn = toast.querySelector('button');
        closeBtn.addEventListener('click', () => dismissToast(toast));

        container.appendChild(toast);

        if (duration > 0) {
            setTimeout(() => dismissToast(toast), duration);
        }
    };

    function dismissToast(toast) {
        if (!toast || toast.classList.contains('toast-leave')) return;
        toast.classList.add('toast-leave');
        setTimeout(() => {
            if (toast.parentNode) {
                toast.parentNode.removeChild(toast);
            }
        }, 220);
    }

    function escapeHtml(str) {
        if (!str) return '';
        return String(str)
            .replace(/&/g, '&amp;')
            .replace(/</g, '&lt;')
            .replace(/>/g, '&gt;')
            .replace(/"/g, '&quot;')
            .replace(/'/g, '&#039;');
    }

    // --- Live Stream Pause / Resume Controls ---
    window.toggleLiveStream = function() {
        isStreamPaused = !isStreamPaused;
        const toggleBtn = document.getElementById('livestream-toggle-btn');
        const streamIndicator = document.getElementById('stream-pulse-dot');
        const streamText = document.getElementById('stream-status-text');

        if (isStreamPaused) {
            if (toggleBtn) {
                toggleBtn.classList.remove('bg-indigo-600/20', 'text-indigo-300', 'border-indigo-500/40');
                toggleBtn.classList.add('bg-amber-600/20', 'text-amber-300', 'border-amber-500/40');
                toggleBtn.innerHTML = `
                    <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M14.752 11.168l-3.197-2.132A1 1 0 0010 9.87v4.263a1 1 0 001.555.832l3.197-2.132a1 1 0 000-1.664z"></path><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 12a9 9 0 11-18 0 9 9 0 0118 0z"></path></svg>
                    <span>Resume Stream</span>
                `;
            }
            if (streamIndicator) {
                streamIndicator.className = 'status-dot bg-amber-500';
            }
            if (streamText) {
                streamText.textContent = 'Stream Paused';
                streamText.className = 'text-amber-400 font-medium';
            }
            window.showToast('info', 'Live Stream', 'Live UI updates paused');
        } else {
            if (toggleBtn) {
                toggleBtn.classList.remove('bg-amber-600/20', 'text-amber-300', 'border-amber-500/40');
                toggleBtn.classList.add('bg-indigo-600/20', 'text-indigo-300', 'border-indigo-500/40');
                toggleBtn.innerHTML = `
                    <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10 9v6m4-6v6m7-3a9 9 0 11-18 0 9 9 0 0118 0z"></path></svg>
                    <span>Pause Stream</span>
                `;
            }
            if (streamIndicator) {
                streamIndicator.className = 'status-dot status-dot-online';
            }
            if (streamText) {
                streamText.textContent = 'Live SSE Active';
                streamText.className = 'text-emerald-400 font-medium';
            }
            window.showToast('success', 'Live Stream', 'Live UI updates resumed');
            triggerDisplaysRefresh();
            triggerHistoryRefresh();
        }
    };

    // --- Server-Sent Events (SSE) Listener ---
    function initSSE() {
        if (eventSource) {
            eventSource.close();
            eventSource = null;
        }

        eventSource = new EventSource('/api/events');

        eventSource.onopen = function() {
            reconnectAttempts = 0;
            updateSSEStatus(true);
        };

        eventSource.onerror = function() {
            updateSSEStatus(false);
            if (eventSource) {
                eventSource.close();
                eventSource = null;
            }
            // Exponential backoff reconnect
            const delay = Math.min(1000 * Math.pow(1.5, reconnectAttempts), 15000);
            reconnectAttempts++;
            clearTimeout(reconnectTimeout);
            reconnectTimeout = setTimeout(initSSE, delay);
        };

        eventSource.addEventListener('stats_updated', function(e) {
            try {
                const stats = JSON.parse(e.data);
                updateStatsUI(stats);
            } catch (err) {
                console.error('Failed to parse stats_updated payload', err);
            }
        });

        eventSource.addEventListener('display_updated', function(e) {
            if (isStreamPaused) return;
            debouncedDisplaysRefresh();
        });

        eventSource.addEventListener('target_added', function(e) {
            if (isStreamPaused) return;
            debouncedDisplaysRefresh();
        });

        eventSource.addEventListener('target_removed', function(e) {
            if (isStreamPaused) return;
            debouncedDisplaysRefresh();
        });

        eventSource.addEventListener('command_logged', function(e) {
            if (isStreamPaused) return;
            triggerHistoryRefresh();
        });

        eventSource.addEventListener('toast', function(e) {
            try {
                const toastData = JSON.parse(e.data);
                window.showToast(toastData.level, toastData.title, toastData.message);
            } catch (err) {
                console.error('Failed to parse toast payload', err);
            }
        });
    }

    function updateSSEStatus(connected) {
        const indicator = document.getElementById('stream-pulse-dot');
        const text = document.getElementById('stream-status-text');

        if (connected) {
            if (!isStreamPaused) {
                if (indicator) indicator.className = 'status-dot status-dot-online';
                if (text) {
                    text.textContent = 'Live SSE Active';
                    text.className = 'text-emerald-400 font-medium';
                }
            }
        } else {
            if (indicator) indicator.className = 'status-dot status-dot-offline';
            if (text) {
                text.textContent = 'Connecting...';
                text.className = 'text-slate-400';
            }
        }
    }

    function updateStatsUI(stats) {
        if (!stats) return;
        const totalEl = document.getElementById('stat-total-displays');
        const activeEl = document.getElementById('stat-active-displays');
        const offlineEl = document.getElementById('stat-offline-displays');
        const commandsEl = document.getElementById('stat-total-commands');

        if (totalEl && stats.total_displays !== undefined) totalEl.textContent = stats.total_displays;
        if (activeEl && stats.active_displays !== undefined) activeEl.textContent = stats.active_displays;
        if (offlineEl && stats.offline_displays !== undefined) offlineEl.textContent = stats.offline_displays;
        if (commandsEl && stats.total_commands !== undefined) commandsEl.textContent = stats.total_commands;
    }

    function debouncedDisplaysRefresh() {
        clearTimeout(refreshDebounceTimer);
        refreshDebounceTimer = setTimeout(triggerDisplaysRefresh, 300);
    }

    function triggerDisplaysRefresh() {
        if (window.htmx) {
            const grid = document.getElementById('displays-grid');
            if (grid) {
                window.htmx.ajax('GET', '/displays', { target: '#displays-grid', swap: 'innerHTML' });
            }
        }
    }

    function triggerHistoryRefresh() {
        if (window.htmx) {
            const tbody = document.getElementById('history-table-body');
            if (tbody) {
                window.htmx.ajax('GET', '/history', { target: '#history-table-body', swap: 'innerHTML' });
            }
        }
    }

    // --- Synchronized Live Clock & NTP Status ---
    function parseOffsetFromDOM() {
        const offsetEl = document.getElementById('ntp-offset-data');
        if (offsetEl && offsetEl.dataset.offsetNanos) {
            const nanos = parseInt(offsetEl.dataset.offsetNanos, 10);
            if (!isNaN(nanos)) {
                ntpOffsetNanos = nanos;
            }
        }
    }

    function updateClock() {
        const clockEl = document.getElementById('digital-clock');
        const dateEl = document.getElementById('digital-date');
        const tzEl = document.getElementById('digital-timezone');
        const ntpOffsetEl = document.getElementById('ntp-offset-pill');

        if (!clockEl) return;

        const nowLocal = Date.now();
        const offsetMillis = Math.round(ntpOffsetNanos / 1000000);
        const syncDate = new Date(nowLocal + offsetMillis);

        const hours = String(syncDate.getHours()).padStart(2, '0');
        const minutes = String(syncDate.getMinutes()).padStart(2, '0');
        const seconds = String(syncDate.getSeconds()).padStart(2, '0');

        clockEl.textContent = `${hours}:${minutes}:${seconds}`;

        if (dateEl) {
            const options = { weekday: 'short', year: 'numeric', month: 'short', day: 'numeric' };
            dateEl.textContent = syncDate.toLocaleDateString(undefined, options);
        }

        if (tzEl) {
            const tzName = Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
            const offsetHours = -syncDate.getTimezoneOffset() / 60;
            const sign = offsetHours >= 0 ? '+' : '';
            tzEl.textContent = `${tzName} (UTC${sign}${offsetHours})`;
        }

        if (ntpOffsetEl) {
            const absOffsetMs = Math.abs(offsetMillis);
            if (absOffsetMs < 500) {
                ntpOffsetEl.className = 'px-2 py-0.5 text-xs rounded-full font-mono badge-online';
                ntpOffsetEl.textContent = `NTP Synced (${absOffsetMs}ms)`;
            } else {
                ntpOffsetEl.className = 'px-2 py-0.5 text-xs rounded-full font-mono badge-offline';
                ntpOffsetEl.textContent = `NTP Offset (${offsetMillis}ms)`;
            }
        }
    }

    // --- Client-Side Display Filtering & Search ---
    window.filterDisplays = function() {
        const searchInput = document.getElementById('display-search-input');
        const statusSelect = document.getElementById('display-status-filter');
        const emptyState = document.getElementById('search-empty-state');
        const matchCountEl = document.getElementById('search-match-count');

        const query = (searchInput ? searchInput.value : '').toLowerCase().trim();
        const statusFilter = statusSelect ? statusSelect.value : 'all';

        const cards = document.querySelectorAll('.display-card');
        let visibleCount = 0;

        cards.forEach(card => {
            const targetId = (card.getAttribute('data-target-id') || '').toLowerCase();
            const targetName = (card.getAttribute('data-target-name') || '').toLowerCase();
            const targetChannel = (card.getAttribute('data-target-channel') || '').toLowerCase();
            const targetUrl = (card.getAttribute('data-target-url') || '').toLowerCase();
            const isActive = card.getAttribute('data-active') === 'true';

            const matchesQuery = !query || 
                targetId.includes(query) || 
                targetName.includes(query) || 
                targetChannel.includes(query) || 
                targetUrl.includes(query);

            let matchesStatus = true;
            if (statusFilter === 'online') {
                matchesStatus = isActive;
            } else if (statusFilter === 'offline') {
                matchesStatus = !isActive;
            }

            if (matchesQuery && matchesStatus) {
                card.style.display = '';
                visibleCount++;
            } else {
                card.style.display = 'none';
            }
        });

        if (emptyState) {
            if (cards.length > 0 && visibleCount === 0) {
                emptyState.classList.remove('hidden');
            } else {
                emptyState.classList.add('hidden');
            }
        }

        if (matchCountEl) {
            matchCountEl.textContent = `${visibleCount} of ${cards.length} displays`;
        }
    };

    // --- Modal and Lightbox Helpers ---
    window.closeModal = function() {
        const modalContainer = document.getElementById('modal-container');
        if (modalContainer) {
            modalContainer.innerHTML = '';
        }
    };

    document.addEventListener('keydown', function(e) {
        if (e.key === 'Escape') {
            window.closeModal();
        }
    });

    // --- Lifecycle and HTMX Hooks ---
    document.addEventListener('DOMContentLoaded', function() {
        parseOffsetFromDOM();
        updateClock();
        setInterval(updateClock, 200);
        initSSE();

        // Bind search input listeners
        const searchInput = document.getElementById('display-search-input');
        if (searchInput) {
            searchInput.addEventListener('input', window.filterDisplays);
        }

        const statusSelect = document.getElementById('display-status-filter');
        if (statusSelect) {
            statusSelect.addEventListener('change', window.filterDisplays);
        }
    });

    document.addEventListener('htmx:afterSwap', function(evt) {
        parseOffsetFromDOM();
        window.filterDisplays();
    });

    document.addEventListener('htmx:responseError', function(evt) {
        const errorText = evt.detail.xhr ? (evt.detail.xhr.responseText || evt.detail.xhr.statusText) : 'Unknown error';
        window.showToast('error', 'Request Failed', errorText.substring(0, 120));
    });

})();
