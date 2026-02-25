var QuestionDetailView = (function () {
    function render(container, spaceId, questionId) {
        Dom.clear(container);

        var headerEl = Dom.el('div', { class: 'detail-header', id: 'q-header' });
        container.appendChild(headerEl);

        var content = Dom.el('div', { id: 'q-content' });
        container.appendChild(content);

        loadQuestion(spaceId, questionId);
        loadResults(spaceId, questionId);
    }

    function loadQuestion(spaceId, questionId) {
        Api.get('/api/dossiers/' + spaceId + '/questions').then(function (questions) {
            var q = (questions || []).find(function (x) { return x.id === questionId; });
            if (!q) return;
            var header = document.getElementById('q-header');
            if (!header) return;
            Dom.clear(header);

            var keywords = (q.keywords || '').split(/\s+/).filter(Boolean);
            var keywordBadges = keywords.map(function (kw) {
                return Dom.el('span', { class: 'badge badge-unchanged' }, [kw]);
            });

            header.appendChild(Dom.el('div', { class: 'detail-header-info' }, [
                Dom.el('h1', { class: 'detail-title' }, [q.text]),
                Dom.el('div', { class: 'detail-meta' }, keywordBadges.concat([
                    Dom.el('span', { style: 'color: var(--text-muted);' },
                        [String(q.total_results || 0) + ' r\u00e9sultats'])
                ]))
            ]));
            header.appendChild(Dom.el('div', { class: 'page-actions' }, [
                Dom.el('button', { class: 'btn btn-primary', onClick: function () {
                    Api.post('/api/dossiers/' + spaceId + '/questions/' + questionId + '/run').then(function (res) {
                        Toast.success(res.new_results + ' nouveaux r\u00e9sultats');
                        loadResults(spaceId, questionId);
                        loadQuestion(spaceId, questionId);
                    });
                }}, ['Ex\u00e9cuter maintenant'])
            ]));
        });
    }

    function loadResults(spaceId, questionId) {
        var content = document.getElementById('q-content');
        if (!content) return;
        Dom.clear(content);
        content.appendChild(Dom.el('div', { class: 'loading' }, [Dom.el('span', { class: 'spinner' }), 'Chargement...']));

        Api.get('/api/dossiers/' + spaceId + '/questions/' + questionId + '/results?limit=50').then(function (results) {
            Dom.clear(content);
            if (!results || results.length === 0) {
                content.appendChild(Dom.el('div', { class: 'empty-state' }, [
                    Dom.el('div', { class: 'empty-state-text' }, ['Aucun r\u00e9sultat. Ex\u00e9cutez la question.'])
                ]));
                return;
            }

            results.forEach(function (ext) {
                var preview = Dom.el('div', { class: 'extraction-preview', style: 'display: none;' });
                preview.textContent = ext.extracted_text || '(vide)';

                var card = Dom.el('div', { class: 'card', style: 'margin-bottom: 8px; cursor: pointer;', onClick: function () {
                    preview.style.display = preview.style.display === 'none' ? 'block' : 'none';
                }}, [
                    Dom.el('div', { style: 'display: flex; justify-content: space-between; align-items: center;' }, [
                        Dom.el('span', { style: 'font-weight: 500;' }, [ext.title || '(sans titre)']),
                        Dom.el('span', { class: 'time-relative' }, [relTime(ext.extracted_at)])
                    ]),
                    ext.url ? Dom.el('div', { class: 'cell-url', style: 'margin-top: 4px;' }, [ext.url]) : Dom.text(''),
                    preview
                ]);
                content.appendChild(card);
            });
        });
    }

    return { render: render };
})();
