var QuestionsView = (function () {
    var currentSpaceId = null;

    function render(container, spaceId) {
        currentSpaceId = spaceId;
        Dom.clear(container);

        container.appendChild(Dom.el('div', { class: 'page-header' }, [
            Dom.el('h1', { class: 'page-title' }, ['Questions']),
            Dom.el('div', { class: 'page-actions' }, [
                Dom.el('button', { class: 'btn btn-primary', onClick: showAddForm }, ['+ Ajouter'])
            ])
        ]));

        var tableWrap = Dom.el('div', { id: 'questions-table' });
        container.appendChild(tableWrap);

        loadQuestions(spaceId);
    }

    function loadQuestions(spaceId) {
        Api.get('/api/dossiers/' + spaceId + '/questions').then(function (questions) {
            State.set('questions', questions || []);
            renderTable(questions || []);
        });
    }

    function renderTable(questions) {
        var wrap = document.getElementById('questions-table');
        if (!wrap) return;
        Dom.clear(wrap);

        if (questions.length === 0) {
            wrap.appendChild(Dom.el('div', { class: 'empty-state' }, [
                Dom.el('div', { class: 'empty-state-icon' }, ['\u{2753}']),
                Dom.el('div', { class: 'empty-state-text' }, ['Aucune question. Ajoutez des recherches r\u00e9currentes.']),
                Dom.el('button', { class: 'btn btn-primary', onClick: showAddForm }, ['Ajouter une question'])
            ]));
            return;
        }

        var table = Dom.el('table', { class: 'data-table' });
        table.appendChild(Dom.el('thead', {}, [
            Dom.el('tr', {}, [
                Dom.el('th', {}, ['Question']),
                Dom.el('th', {}, ['Keywords']),
                Dom.el('th', {}, ['Fr\u00e9quence']),
                Dom.el('th', {}, ['R\u00e9sultats']),
                Dom.el('th', {}, ['Derni\u00e8re ex\u00e9cution']),
                Dom.el('th', {}, ['Actif']),
                Dom.el('th', {}, ['Actions'])
            ])
        ]));

        var tbody = Dom.el('tbody');
        questions.forEach(function (q) {
            var toggle = Dom.el('input', {
                type: 'checkbox',
                class: 'form-toggle',
                onClick: function (e) {
                    e.stopPropagation();
                    Api.put('/api/dossiers/' + currentSpaceId + '/questions/' + q.id,
                        { enabled: e.target.checked });
                }
            });
            if (q.enabled) toggle.checked = true;

            var scheduleHours = Math.round((q.schedule_ms || 86400000) / 3600000);
            var scheduleText = scheduleHours >= 24 ? Math.round(scheduleHours / 24) + 'j' : scheduleHours + 'h';

            var row = Dom.el('tr', { class: 'clickable', onClick: function () {
                Router.navigate(currentSpaceId + '/questions/' + q.id);
            }}, [
                Dom.el('td', {}, [truncate(q.text, 50)]),
                Dom.el('td', { class: 'cell-mono', style: 'font-size: var(--font-size-xs);' }, [q.keywords || '-']),
                Dom.el('td', {}, [scheduleText]),
                Dom.el('td', {}, [String(q.total_results || 0)]),
                Dom.el('td', { class: 'time-relative' }, [relTime(q.last_run_at)]),
                Dom.el('td', {}, [toggle]),
                Dom.el('td', { class: 'cell-actions' }, [
                    Dom.el('button', { class: 'btn btn-ghost btn-sm', title: 'Ex\u00e9cuter', onClick: function (e) {
                        e.stopPropagation();
                        Api.post('/api/dossiers/' + currentSpaceId + '/questions/' + q.id + '/run').then(function (res) {
                            Toast.success(res.new_results + ' nouveaux r\u00e9sultats');
                            loadQuestions(currentSpaceId);
                        });
                    }}, ['\u{25b6}']),
                    Dom.el('button', { class: 'btn btn-danger btn-sm', title: 'Supprimer', onClick: function (e) {
                        e.stopPropagation();
                        Modal.confirm('Supprimer cette question ?',
                            'La question et ses r\u00e9sultats seront supprim\u00e9s.',
                            function () {
                                Api.del('/api/dossiers/' + currentSpaceId + '/questions/' + q.id).then(function () {
                                    Toast.success('Question supprim\u00e9e');
                                    loadQuestions(currentSpaceId);
                                });
                            });
                    }}, ['\u{2715}'])
                ])
            ]);
            tbody.appendChild(row);
        });
        table.appendChild(tbody);
        wrap.appendChild(table);
    }

    function showAddForm() {
        Modal.form('Nouvelle question', [
            { name: 'text', label: 'Question', type: 'text', required: true, placeholder: 'Ex: tendances IA 2026' },
            { name: 'keywords', label: 'Mots-cl\u00e9s', type: 'text', placeholder: 'IA LLM inference 2026' },
            { name: 'schedule_hours', label: 'Fr\u00e9quence (heures)', type: 'number', value: 24 },
            { name: 'max_results', label: 'R\u00e9sultats max', type: 'number', value: 20 }
        ], function (data) {
            Api.post('/api/dossiers/' + currentSpaceId + '/questions', {
                text: data.text,
                keywords: data.keywords,
                channels: '[]',
                schedule_ms: Math.round(data.schedule_hours * 3600000),
                max_results: data.max_results,
                follow_links: true
            }).then(function () {
                Toast.success('Question ajout\u00e9e');
                loadQuestions(currentSpaceId);
            });
        });
    }

    return { render: render };
})();
