        function portfolio() {
            return {
                scrolled: false,
                mobileMenuOpen: false,
                postModalOpen: false,
                loginModalOpen: false,
                adminPostModalOpen: false,
                adminDashboardModalOpen: false,
                adminEditPostModalOpen: false,

                init() {
                    const updateScrollLock = () => {
                        const isLocked = this.postModalOpen || this.loginModalOpen || this.adminPostModalOpen || this.adminDashboardModalOpen || this.adminEditPostModalOpen;
                        if (isLocked) {
                            document.documentElement.classList.add('modal-open');
                            document.body.classList.add('modal-open');
                            document.documentElement.style.overflow = 'hidden';
                            document.body.style.overflow = 'hidden';
                        } else {
                            document.documentElement.classList.remove('modal-open');
                            document.body.classList.remove('modal-open');
                            document.documentElement.style.overflow = '';
                            document.body.style.overflow = '';
                        }
                    };

                    this.$watch('postModalOpen', updateScrollLock);
                    this.$watch('loginModalOpen', updateScrollLock);
                    this.$watch('adminPostModalOpen', updateScrollLock);
                    this.$watch('adminDashboardModalOpen', updateScrollLock);
                    this.$watch('adminEditPostModalOpen', updateScrollLock);

                    const sentinel = document.getElementById('scroll-sentinel');
                    if (sentinel) {
                        const scrollObserver = new IntersectionObserver((entries) => {
                            this.scrolled = !entries[0].isIntersecting;
                        }, { threshold: 0 });
                        scrollObserver.observe(sentinel);
                    }

                    document.addEventListener('click', (e) => {
                        const link = e.target.closest('a[href^="#"]');
                        if (link) {
                            e.preventDefault();
                            const href = link.getAttribute('href');
                            if (href === '#') {
                                window.scrollTo({ top: 0, behavior: 'smooth' });
                                if (history.pushState) {
                                    history.pushState(null, null, ' ');
                                }
                                return;
                            }
                            const target = document.querySelector(href);
                            if (target) {
                                if (history.pushState) {
                                    history.pushState(null, null, href);
                                }
                                target.scrollIntoView({ behavior: 'smooth' });
                            }
                        }
                    });

                    const handleInitialHash = () => {
                        if (window.location.hash && window.location.hash !== '#') {
                            const target = document.querySelector(window.location.hash);
                            if (target) {
                                target.scrollIntoView({ behavior: 'smooth' });
                            }
                        }
                    };

                    if (document.fonts) {
                        document.fonts.ready.then(handleInitialHash);
                    } else {
                        window.addEventListener('load', handleInitialHash);
                    }

                    // Dynamic background intersection observer
                    const sections = document.querySelectorAll('[data-theme-color]');
                    const themeObserver = new IntersectionObserver((entries) => {
                        entries.forEach(entry => {
                            if (entry.isIntersecting) {
                                const color = entry.target.getAttribute('data-theme-color');
                                document.documentElement.setAttribute('data-bg', color);
                            }
                        });
                    }, {
                        rootMargin: '-40% 0px -50% 0px',
                        threshold: 0
                    });

                    sections.forEach(sec => themeObserver.observe(sec));

                    // Re-run for HTMX injected content if needed
                    document.body.addEventListener('htmx:afterSwap', (event) => {
                        if (event.target && event.target.matches('section[hx-get]')) {
                            event.target.setAttribute('data-loaded', 'true');
                        }
                        const newSections = event.target.querySelectorAll('[data-theme-color]');
                        newSections.forEach(sec => themeObserver.observe(sec));
                    });
                }
            }
        }

        function showHtmxSectionError(section) {
            section.innerHTML = '<div class="container"><div class="form__alert form__alert--error" role="alert"><span>[ ERROR LOADING SECTION ]</span><button type="button" class="btn" data-section-retry>RETRY</button></div></div>';
        }

        function showHtmxContactError(target, html) {
            target.innerHTML = html || '<div class="form__alert form__alert--error" role="alert">[ ERROR SENDING MESSAGE ]</div>';
        }

        document.body.addEventListener('htmx:responseError', (event) => {
            const target = event.detail.target;
            if (!target) return;
            if (target.id === 'contact-response') {
                showHtmxContactError(target, event.detail.xhr && event.detail.xhr.responseText);
                return;
            }
            if (target.matches('section[hx-get]')) {
                showHtmxSectionError(target);
            }
        });

        document.body.addEventListener('htmx:sendError', (event) => {
            const target = event.detail.target;
            if (!target) return;
            if (target.id === 'contact-response') {
                showHtmxContactError(target);
                return;
            }
            if (target.matches('section[hx-get]')) {
                showHtmxSectionError(target);
            }
        });

        document.addEventListener('click', (event) => {
            const retry = event.target.closest('[data-section-retry]');
            if (!retry) return;
            const section = retry.closest('section[hx-get]');
            if (section) {
                htmx.trigger(section, 'revealed');
            }
        });

        function readCookie(name) {
            const prefix = name + '=';
            for (const part of document.cookie.split(';')) {
                const value = part.trim();
                if (value.startsWith(prefix)) {
                    return decodeURIComponent(value.slice(prefix.length));
                }
            }
            return '';
        }

        document.body.addEventListener('htmx:configRequest', (event) => {
            const csrfToken = readCookie('csrf_token');
            if (csrfToken) {
                event.detail.headers['X-CSRF-Token'] = csrfToken;
            }
        });
