# Christian Moreno - Portfolio

A brutally elegant, Web3-inspired personal portfolio designed for impact. Featuring a dynamic theme-switching background, huge editorial typography, and an extreme tech-brutalist wireframe aesthetic.

## 🚀 Tech Stack

- **Backend:** [Go (Golang)](https://go.dev/) - Blazing fast standard `net/http` server
- **Database:** [SQLite](https://sqlite.org/index.html) - Embedded local database (`portfolio.db`) storing profile, experience, and skills
- **Frontend Interactivity:** [HTMX](https://htmx.org/) - Loading sections and sending contact forms dynamically without heavy JS frameworks
- **Frontend Logic:** [Alpine.js](https://alpinejs.dev/) - Intersection observers and lightweight state management
- **Design System:** Custom CSS using CSS variables, brutalist grids, and dynamic scrolling themes

## 📸 Previews

### Hero Section (Dark Theme)
![Hero](static/img/screenshot-1-hero.png)

### About Section (White Theme)
![About](static/img/screenshot-2-about.png)

### Skills Section (Electric Blue Theme)
![Skills](static/img/screenshot-3-skills.png)

## 🛠 Features

- **Dynamic Theme Transitions:** As you scroll through the page, Alpine.js's IntersectionObserver detects the current section and smoothly transitions the entire page's background and accent colors between Black, White, and Electric Blue.
- **Server-Side Rendered Partials:** Uses HTMX to lazy-load the `About`, `Skills`, `Experience`, `Education`, and `Contact` sections directly from the Go server on scroll.
- **Anti-Slop Design:** Stripped of standard SaaS clichés (no rounded corners, no soft shadows, no gradients). Uses stark 1px solid borders, monospaced tech metadata (`Space Mono`), and massive editorial display typography (`Instrument Serif`).
- **Contact Form Validation:** Fully functional contact form with Alpine.js frontend validation and Go backend verification (persisted to SQLite).

## 💻 How to Run Locally

### Prerequisites
- [Go](https://go.dev/doc/install) (1.20+ recommended)

### Installation & Execution

1. **Clone the repository:**
   ```bash
   git clone https://github.com/C4risrocks/ola-k-ase.git
   cd ola-k-ase
   ```

2. **Run the server:**
   ```bash
   go run .
   ```
   *Note: This command will automatically compile the Go code and parse the templates.*

3. **View the site:**
   Open your browser and navigate to:
   [http://localhost:8080](http://localhost:8080)

4. **Running Tests:**
   Ensure the endpoints and partials are rendering correctly by running the Go tests:
   ```bash
   go test ./...
   ```

## 🏗 Project Structure

```text
.
├── database.go           # SQLite database initialization and seeding
├── main.go               # HTTP server, routing, and HTMX handlers
├── main_test.go          # Tests for the HTTP handlers
├── portfolio.db          # Auto-generated SQLite database
├── static/
│   ├── css/style.css     # The brutalist design system CSS
│   └── img/              # Images, avatars, and screenshots
└── templates/
    ├── index.html        # Base layout, Nav, Hero, and Alpine.js logic
    └── partials/         # HTMX-loaded HTML fragments (About, Skills, etc.)
```

## 📜 License

MIT License. See `LICENSE` for more information.
