var SourceDetailView = (function () {
    var activeTab = 'extractions';

    function render(container, spaceId, sourceId) {
        Dom.clear(container);
        activeTab = 'extractions';

        var headerEl = Dom.el('div', { class: 'detail-header', id: 'src-header' });
        container.appendChild(headerEl);

        var tabs = Dom.el('div', { class: 'section-tabs' }, [
            tabBtn('Extractions', 'extractions', function () { activeTab = 'extractions'; loadExtractions(spaceId, sourceId); }),
            tabBtn('Historique', 'history', function () { activeTab = 'history'; loadHistory(spaceId, sourceId); })
        ]);
        container.appendChild(tabs);

        var content = Dom.el('div', { id: 'src-content' });
        container.appendChild(content);

        loadSource(spaceId, sourceId);
        loadExtractions(spaceId, sourceId);
    }

    function tabBtn(label, id, onClick) {
        var btn = Dom.el('button', {
            class: 'section-tab' + (activeTab === id ? ' active' : ''),
            dataset: { tab: id },
            onClick: function () {
                document.querySelectorAll('.section-tab').forEach(function (t) { t.classList.remove('active'); });
                btn.classList.add('active');
                onClick();
            }
        }, [label]);
        return btn;
    }

    function loadSource(spaceId, sourceId) {
        Api.get('/api/dossiers/' + spaceId + '/sources').then(function (sources) {
            var src = (sources || []).find(function (s) { return s.id === sourceId; });
            if (!src) return;
            var header = document.getElementById('src-header');
            if (!header) return;
            Dom.clear(header);

            header.appendChild(Dom.el('div', { class: 'detail-header-info' }, [
                Dom.el('h1', { class: 'detail-title' }, [src.name || src.url]),
                Dom.el('div', { class: 'detail-meta' }, [
                    typeBadge(src.source_type),
                    statusBadge(src.last_status),
                    Dom.el('a', { href: src.url, target: '_blank', style: 'font-family: var(--font-mono); font-size: var(--font-size-xs);' }, [truncate(src.url, 60)])
                ])
            ]));
            header.appendChild(Dom.el('div', { class: 'page-actions' }, [
                Dom.el('button', { class: 'btn btn-primary', onClick: function () {
                    Api.post('/api/dossiers/' + spaceId + '/sources/' + sourceId + '/fetch').then(function () {
                        Toast.success('Fetch lanc\u00e9');
                        loadSource(spaceId, sourceId);
                        if (activeTab === 'extractions') loadExtractions(spaceId, sourceId);
                        else loadHistory(spaceId, sourceId);
                    });
                }}, ['Fetch Now'])
            ]));
        });
    }

    function loadExtractions(spaceId, sourceId) {
        var content = document.getElementById('src-content');
        if (!content) return;
        Dom.clear(content);
        content.appendChild(Dom.el('div', { class: 'loading' }, [Dom.el('span', { class: 'spinner' }), 'Chargement...']));

        Api.get('/api/dossiers/' + spaceId + '/sources/' + sourceId + '/extractions?limit=50').then(function (exts) {
            Dom.clear(content);
            if (!exts || exts.length === 0) {
                content.appendChild(Dom.el('div', { class: 'empty-state' }, [
                    Dom.el('div', { class: 'empty-state-text' }, ['Aucune extraction. Lancez un fetch.'])
                ]));
                return;
            }

            exts.forEach(function (ext) {
                var preview = Dom.el('div', { class: 'extraction-preview', style: 'display: none;' });
                preview.textContent = ext.extracted_text || '(vide)';

                var card = Dom.el('div', { class: 'card', style: 'margin-bottom: 8px; cursor: pointer;', onClick: function () {
                    preview.style.display = preview.style.display === 'none' ? 'block' : 'none';
                }}, [
                    Dom.el('div', { style: 'display: flex; justify-content: space-between; align-items: center;' }, [
                        Dom.el('span', { style: 'font-weight: 500;' }, [ext.title || '(sans titre)']),
                        Dom.el('span', { class: 'time-relative' }, [relTime(ext.extracted_at)])
                    ]),
                    Dom.el('div', { class: 'card-meta', style: 'margin-top: 4px;' }, [
                        'Hash: ' + (ext.content_hash || '').slice(0, 12) + '...'
                    ]),
                    preview
                ]);
                content.appendChild(card);
            });
        });
    }

    function loadHistory(spaceId, sourceId) {
        var content = document.getElementById('src-content');
        if (!content) return;
        Dom.clear(content);
        content.appendChild(Dom.el('div', { class: 'loading' }, [Dom.el('span', { class: 'spinner' }), 'Chargement...']));

        Api.get('/api/dossiers/' + spaceId + '/sources/' + sourceId + '/history?limit=50').then(function (hist) {
            Dom.clear(content);
            if (!hist || hist.length === 0) {
                content.appendChild(Dom.el('div', { class: 'empty-state' }, [
                    Dom.el('div', { class: 'empty-state-text' }, ['Aucun historique.'])
                ]));
                return;
            }

            var table = Dom.el('table', { class: 'data-table' });
            table.appendChild(Dom.el('thead', {}, [
                Dom.el('tr', {}, [
                    Dom.el('th', {}, ['Statut']),
                    Dom.el('th', {}, ['Code HTTP']),
                    Dom.el('th', {}, ['Dur\u00e9e']),
                    Dom.el('th', {}, ['Date']),
                    Dom.el('th', {}, ['Erreur'])
                ])
            ]));

            var tbody = Dom.el('tbody');
            hist.forEach(function (h) {
                tbody.appendChild(Dom.el('tr', {}, [
                    Dom.el('td', {}, [statusBadge(h.status)]),
                    Dom.el('td', { class: 'cell-mono' }, [String(h.status_code || '-')]),
                    Dom.el('td', { class: 'cell-mono' }, [h.duration_ms ? h.duration_ms + 'ms' : '-']),
                    Dom.el('td', { class: 'time-relative' }, [relTime(h.fetched_at)]),
                    Dom.el('td', { style: 'color: var(--danger); font-size: var(--font-size-xs); max-width: 300px; overflow: hidden; text-overflow: ellipsis;' },
                        [h.error_message || ''])
                ]));
            });
            table.appendChild(tbody);
            content.appendChild(table);
        });
    }

    return { render: render };
})();
