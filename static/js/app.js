        function portfolio() {
            return {
                scrolled: false,
                mobileMenuOpen: false,
                isMobile: window.matchMedia('(max-width: 767px)').matches,
                activeSection: 'hero',
                postModalOpen: false,
                loginModalOpen: false,
                adminPostModalOpen: false,
                adminDashboardModalOpen: false,
                adminEditPostModalOpen: false,

                replaySignal() {
                    if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) return;
                    document.querySelectorAll('.network-signal path').forEach(path => {
                        path.getAnimations().forEach(animation => {
                            animation.currentTime = 0;
                            animation.play();
                        });
                    });
                },

                init() {
                    let lockedScrollY = 0;
                    let wasLocked = false;
                    let previousFocus = null;
                    const motionPreference = window.matchMedia('(prefers-reduced-motion: reduce)');
                    const scrollBehavior = () => motionPreference.matches ? 'instant' : 'smooth';
                    const updateScrollLock = () => {
                        const isLocked = this.postModalOpen || this.loginModalOpen || this.adminPostModalOpen || this.adminDashboardModalOpen || this.adminEditPostModalOpen;
                        if (isLocked) {
                            if (!wasLocked) {
                                lockedScrollY = window.scrollY;
                                previousFocus = document.activeElement;
                            }
                            document.documentElement.classList.add('modal-open');
                            document.body.classList.add('modal-open');
                            document.documentElement.style.overflow = 'hidden';
                            document.body.style.overflow = 'hidden';
                            this.$nextTick(() => requestAnimationFrame(() => {
                                const dialog = Array.from(document.querySelectorAll('.modal.active')).pop();
                                if (dialog && !dialog.contains(document.activeElement)) {
                                    dialog.querySelector('button, input, a[href]')?.focus({ preventScroll: true });
                                }
                            }));
                        } else if (wasLocked) {
                            document.documentElement.classList.remove('modal-open');
                            document.body.classList.remove('modal-open');
                            document.documentElement.style.overflow = '';
                            document.body.style.overflow = '';
                            const html = document.documentElement;
                            const prevBehavior = html.style.scrollBehavior;
                            html.style.scrollBehavior = 'auto';
                            window.scrollTo(0, lockedScrollY);
                            html.style.scrollBehavior = prevBehavior;
                            this.$nextTick(() => previousFocus?.focus({ preventScroll: true }));
                        }
                        wasLocked = isLocked;
                        document.querySelectorAll('body > nav, body > main, body > section, body > footer, .skip-link').forEach(element => {
                            element.inert = isLocked;
                        });
                    };

                    this.$watch('postModalOpen', updateScrollLock);
                    this.$watch('loginModalOpen', updateScrollLock);
                    this.$watch('adminPostModalOpen', updateScrollLock);
                    this.$watch('adminDashboardModalOpen', updateScrollLock);
                    this.$watch('adminEditPostModalOpen', updateScrollLock);
                    this.$watch('mobileMenuOpen', open => {
                        document.body.classList.toggle('menu-open', open);
                        document.querySelectorAll('body > main, body > section, body > footer').forEach(element => {
                            element.inert = open || wasLocked;
                        });
                        this.$nextTick(() => {
                            if (open) {
                                document.querySelector('.nav__links .nav__link')?.focus({ preventScroll: true });
                            } else if (document.activeElement.closest('.nav__links')) {
                                document.querySelector('.nav__burger')?.focus({ preventScroll: true });
                            }
                        });
                    });
                    window.matchMedia('(max-width: 767px)').addEventListener('change', event => {
                        this.isMobile = event.matches;
                        this.mobileMenuOpen = false;
                    });

                    document.addEventListener('keydown', event => {
                        if (event.key !== 'Tab') return;
                        const dialog = Array.from(document.querySelectorAll('.modal.active')).pop()
                            || (this.mobileMenuOpen ? document.getElementById('main-nav') : null);
                        if (!dialog) return;
                        const controls = Array.from(dialog.querySelectorAll('a[href], button, input, textarea, select, [tabindex="0"]'))
                            .filter(element => !element.disabled && element.getClientRects().length);
                        const first = controls[0];
                        const last = controls[controls.length - 1];
                        if (!first) return;
                        if (event.shiftKey && (document.activeElement === first || !dialog.contains(document.activeElement))) {
                            event.preventDefault();
                            last.focus();
                        } else if (!event.shiftKey && (document.activeElement === last || !dialog.contains(document.activeElement))) {
                            event.preventDefault();
                            first.focus();
                        }
                    });

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
                                window.scrollTo({ top: 0, behavior: scrollBehavior() });
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
                                target.scrollIntoView({ behavior: scrollBehavior() });
                                if (link.classList.contains('skip-link')) {
                                    target.setAttribute('tabindex', '-1');
                                    target.focus({ preventScroll: true });
                                }
                            }
                        }
                    });

                    const handleInitialHash = () => {
                        if (window.location.hash && window.location.hash !== '#') {
                            const target = document.querySelector(window.location.hash);
                            if (target) {
                                target.scrollIntoView({ behavior: scrollBehavior() });
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
                                if (entry.target.id) this.activeSection = entry.target.id;
                            }
                        });
                    }, {
                        rootMargin: '-40% 0px -50% 0px',
                        threshold: 0
                    });

                    sections.forEach(sec => themeObserver.observe(sec));

                    // Re-run for HTMX injected content if needed
                    document.body.addEventListener('htmx:afterSwap', (event) => {
                        const dialog = event.target.closest('.modal.active');
                        if (dialog && !dialog.contains(document.activeElement)) {
                            dialog.querySelector('button, input, a[href]')?.focus({ preventScroll: true });
                        }
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
                htmx.ajax('GET', section.getAttribute('hx-get'), { target: section, swap: 'innerHTML' });
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
