/* Chat page boot: embed-mode + share meta (external for strict CSP). */
(() => {
        (() => {
            if (new URLSearchParams(window.location.search).get('embed') === '1') {
                document.documentElement.classList.add('embed-mode');
            }
        })();
        (() => {
            const shareImageURL = new URL('./assets/001.png', window.location.href).href;
            const sharePageURL = window.location.href.split('#')[0];
            const metas = [
                ['meta[property="og:image"]', 'content', shareImageURL],
                ['meta[itemprop="image"]', 'content', shareImageURL],
                ['meta[property="og:url"]', 'content', sharePageURL],
                ['link[rel="image_src"]', 'href', shareImageURL]
            ];
            metas.forEach(([selector, attr, value]) => {
                const el = document.querySelector(selector);
                if (el) el.setAttribute(attr, value);
            });
        })();
})();
