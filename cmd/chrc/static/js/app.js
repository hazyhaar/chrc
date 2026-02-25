// --- Shared helpers (used by views) ---

function typeBadge(type) {
    var cls = 'badge badge-' + (type || 'web');
    return Dom.el('span', { class: cls }, [type || 'web']);
}

function statusBadge(status) {
    var map = { ok: 'ok', error: 'error', pending: 'pending', unchanged: 'unchanged', no_change: 'unchanged' };
    var cls = 'badge badge-' + (map[status] || 'pending');
    return Dom.el('span', { class: cls }, [status || 'pending']);
}

function relTime(ts) {
    if (!ts) return Dom.text('jamais');
    var diff = Date.now() - ts;
    if (diff < 60000) return Dom.text('il y a ' + Math.round(diff / 1000) + 's');
    if (diff < 3600000) return Dom.text('il y a ' + Math.round(diff / 60000) + 'min');
    if (diff < 86400000) return Dom.text('il y a ' + Math.round(diff / 3600000) + 'h');
    return Dom.text('il y a ' + Math.round(diff / 86400000) + 'j');
}

function truncate(str, len) {
    if (!str) return '';
    return str.length > len ? str.slice(0, len) + '...' : str;
}

// --- Sidebar ---

function renderSidebar(spaceId) {
    var sidebar = document.getElementById('sidebar');
    if (!spaceId) {
        sidebar.classList.add('hidden');
        return;
    }
    sidebar.classList.remove('hidden');
    Dom.clear(sidebar);

    var route = Router.current();
    var currentView = route.view || '';

    var navItems = [
        { label: 'Tableau de bord', view: '', icon: '\u25a3' },
        { label: 'Sources', view: 'sources', icon: '\ud83c\udf10' },
        { label: 'Questions', view: 'questions', icon: '\u2753' },
        { label: 'Recherche', view: 'search', icon: '\ud83d\udd0d' }
    ];

    var navSection = Dom.el('div', { class: 'sidebar-section' });
    navSection.appendChild(Dom.el('div', { class: 'sidebar-section-title' }, ['Navigation']));
    var nav = Dom.el('ul', { class: 'sidebar-nav' });

    navItems.forEach(function (item) {
        var active = currentView === item.view;
        var li = Dom.el('li', {
            class: 'sidebar-nav-item' + (active ? ' active' : ''),
            onClick: function () {
                var path = spaceId + (item.view ? '/' + item.view : '');
                Router.navigate(path);
            }
        }, [
            Dom.el('span', { class: 'sidebar-nav-icon' }, [item.icon]),
            Dom.text(item.label)
        ]);
        nav.appendChild(li);
    });
    navSection.appendChild(nav);
    sidebar.appendChild(navSection);

    // Back to spaces link.
    var backSection = Dom.el('div', { class: 'sidebar-section', style: 'margin-top: auto; border-top: 1px solid var(--border);' });
    backSection.appendChild(Dom.el('div', {
        class: 'sidebar-nav-item',
        style: 'cursor: pointer;',
        onClick: function () { Router.navigate(''); }
    }, [
        Dom.el('span', { class: 'sidebar-nav-icon' }, ['\u2190']),
        Dom.text('Tous les espaces')
    ]));
    sidebar.appendChild(backSection);
}

// --- Admin sidebar ---

function renderAdminSidebar() {
    var sidebar = document.getElementById('sidebar');
    sidebar.classList.remove('hidden');
    Dom.clear(sidebar);

    var route = Router.current();
    var currentView = route.view || '';

    var navItems = [
        { label: 'Vue d\'ensemble', view: '', icon: '\u25a3' },
        { label: 'Moteurs de recherche', view: 'engines', icon: '\u2699' },
        { label: 'Catalogue de sources', view: 'source-registry', icon: '\ud83d\udcda' },
        { label: 'Utilisateurs', view: 'users', icon: '\ud83d\udc64' }
    ];

    var navSection = Dom.el('div', { class: 'sidebar-section' });
    navSection.appendChild(Dom.el('div', { class: 'sidebar-section-title' }, ['Administration']));
    var nav = Dom.el('ul', { class: 'sidebar-nav' });

    navItems.forEach(function (item) {
        var active = currentView === (item.view || null);
        var li = Dom.el('li', {
            class: 'sidebar-nav-item' + (active ? ' active' : ''),
            onClick: function () {
                Router.navigate('admin' + (item.view ? '/' + item.view : ''));
            }
        }, [
            Dom.el('span', { class: 'sidebar-nav-icon' }, [item.icon]),
            Dom.text(item.label)
        ]);
        nav.appendChild(li);
    });
    navSection.appendChild(nav);
    sidebar.appendChild(navSection);

    // Back to spaces.
    var backSection = Dom.el('div', { class: 'sidebar-section', style: 'margin-top: auto; border-top: 1px solid var(--border);' });
    backSection.appendChild(Dom.el('div', {
        class: 'sidebar-nav-item',
        style: 'cursor: pointer;',
        onClick: function () { Router.navigate(''); }
    }, [
        Dom.el('span', { class: 'sidebar-nav-icon' }, ['\u2190']),
        Dom.text('Retour aux espaces')
    ]));
    sidebar.appendChild(backSection);
}

// --- Header ---

function renderHeader(spaceName) {
    var header = document.getElementById('app-header');
    Dom.clear(header);
    header.appendChild(Dom.el('span', { class: 'header-brand' }, ['veille']));
    if (spaceName) {
        header.appendChild(Dom.el('span', { class: 'header-space-name' }, [spaceName]));
    }
    header.appendChild(Dom.el('div', { class: 'header-spacer' }));

    // Admin link (if admin).
    var user = State.get('user');
    if (user && user.role === 'admin') {
        header.appendChild(Dom.el('button', {
            class: 'btn btn-ghost btn-sm',
            style: 'margin-right: 8px;',
            onClick: function () { Router.navigate('admin'); }
        }, ['Admin']));
    }

    // Logout button.
    if (user) {
        header.appendChild(Dom.el('span', { style: 'color: var(--text-muted); font-size: var(--font-size-sm); margin-right: 12px;' }, [user.name || user.email || '']));
        header.appendChild(Dom.el('button', {
            class: 'btn btn-ghost btn-sm',
            onClick: function () {
                Api.post('/api/auth/logout').then(function () {
                    State.set('user', null);
                    showLogin();
                });
            }
        }, ['Quitter']));
    }
}

// --- Router dispatch ---

function onRouteChange(route) {
    var main = document.getElementById('main-content');

    // Admin routes.
    if (route.admin) {
        renderAdminSidebar();
        renderHeader('Administration');

        switch (route.view) {
        case 'engines':
            AdminEnginesView.render(main);
            break;
        case 'source-registry':
            AdminSourceRegistryView.render(main);
            break;
        case 'users':
            AdminUsersView.render(main);
            break;
        case 'overview':
            // Drill-down: #/admin/overview/userID/spaceID
            if (route.itemId) {
                AdminShardView.render(main, route.itemId);
            } else {
                AdminOverviewView.render(main);
            }
            break;
        default:
            AdminOverviewView.render(main);
        }
        return;
    }

    var spaceId = route.spaceId;
    renderSidebar(spaceId);

    if (!spaceId) {
        renderHeader(null);
        SpacesView.render(main);
        return;
    }

    // Find space name for header.
    var spaces = State.get('spaces') || [];
    var space = spaces.find(function (s) { return s.id === spaceId; });
    renderHeader(space ? space.name : spaceId.slice(0, 8));

    State.set('currentSpaceId', spaceId);

    switch (route.view) {
    case 'sources':
        if (route.itemId) SourceDetailView.render(main, spaceId, route.itemId);
        else SourcesView.render(main, spaceId);
        break;
    case 'questions':
        if (route.itemId) QuestionDetailView.render(main, spaceId, route.itemId);
        else QuestionsView.render(main, spaceId);
        break;
    case 'search':
        SearchView.render(main, spaceId);
        break;
    default:
        DashboardView.render(main, spaceId);
    }
}

// --- Login / App init ---

function showLogin() {
    document.getElementById('sidebar').classList.add('hidden');
    renderHeader(null);
    LoginView.render(document.getElementById('main-content'));
}

function initApp() {
    Api.get('/api/auth/me').then(function (user) {
        State.set('user', user);
        // Load spaces then start router.
        return Api.get('/api/dossiers');
    }).then(function (dossiers) {
        State.set('spaces', dossiers || []);
        EventBus.off('route:change', onRouteChange);
        EventBus.on('route:change', onRouteChange);
        Router.init();
    }).catch(function () {
        // 401 or network error — show login.
        showLogin();
    });
}

// --- Auth:failed handler ---

EventBus.on('auth:failed', function () {
    showLogin();
});

// --- Boot ---

initApp();
