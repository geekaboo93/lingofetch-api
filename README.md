# LingoFetch API

Backend API for LingoFetch - A language learning Chrome extension that captures words and generates AI-powered definitions.

## 🚀 Features

- **AI-Powered Definitions**: Supports multiple AI providers (Gemini, Llama, OpenAI, Claude, Grok, DeepSeek)
- **Notion Integration**: Automatically syncs captured words to Notion databases
- **Multi-language Support**: Detects and handles multiple languages
- **Firestore Storage**: Persistent storage for user data and word history
- **RESTful API**: Clean HTTP API for extension integration

## 🏗️ Tech Stack

- **Language**: Go 1.21+
- **Framework**: Gin (HTTP router)
- **Database**: Google Firestore
- **AI Providers**: Google Gemini, OpenRouter (Llama), and more
- **Deployment**: Google Cloud Run
- **Hot Reload**: Air (development)

## 📋 Prerequisites

- Go 1.21 or higher
- Google Cloud Project with Firestore enabled
- API keys for AI providers (Gemini, OpenRouter, etc.)
- Notion OAuth credentials (for Notion integration)

## 🛠️ Setup

### 1. Clone the Repository

\`\`\`bash
git clone https://github.com/yourusername/lingofetch-api.git
cd lingofetch-api
\`\`\`

### 2. Install Dependencies

\`\`\`bash
go mod download
\`\`\`

### 3. Configure Environment Variables

Copy the example environment file:

\`\`\`bash
cp .env.example .env
\`\`\`

Edit \`.env\` with your credentials:

\`\`\`env
# Google Cloud
GOOGLE_APPLICATION_CREDENTIALS=/path/to/service-account.json
GCP_PROJECT_ID=your-project-id
FIRESTORE_DATABASE_ID=(default)

# AI Provider API Keys
GEMINI_API_KEY=your_gemini_api_key
OPENROUTER_API_KEY=your_openrouter_api_key

# Notion OAuth
NOTION_CLIENT_ID=your_notion_client_id
NOTION_CLIENT_SECRET=your_notion_client_secret
NOTION_REDIRECT_URI=http://localhost:8080/api/v1/auth/notion/callback

# Server
PORT=8080
ENVIRONMENT=development
\`\`\`

### 4. Run the Server

**Development (with hot reload):**

\`\`\`bash
make dev
\`\`\`

**Production:**

\`\`\`bash
make run-api
\`\`\`

The API will be available at \`http://localhost:8080\`

## 📚 API Endpoints

### Core Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | \`/api/v1/capture\` | Capture a word and generate definition |
| GET | \`/api/v1/user/status\` | Get user connection status |
| POST | \`/api/v1/user/settings\` | Update user settings |

### Notion Integration

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | \`/api/v1/auth/notion\` | Initiate Notion OAuth |
| GET | \`/api/v1/auth/notion/callback\` | OAuth callback |
| POST | \`/api/v1/user/notion/sync\` | Sync word to Notion |
| GET | \`/api/v1/user/notion/databases\` | List user's databases |
| POST | \`/api/v1/user/notion/databases\` | Create language database |
| POST | \`/api/v1/user/disconnect\` | Disconnect from Notion |

### Health Check

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | \`/health\` | Health check endpoint |
| GET | \`/api/v1/health\` | API health check |

## 🔧 Development

### Project Structure

\`\`\`
lingofetch-api/
├── cmd/api/              # Application entry point
├── internal/
│   ├── ai/              # AI provider implementations
│   ├── handlers/        # HTTP request handlers
│   ├── models/          # Data models
│   ├── repository/      # Database layer (Firestore)
│   ├── sync/            # Background sync logic
│   └── pkg/             # Shared utilities
├── deployments/         # Deployment configurations
├── docs/                # API documentation
├── .air.toml           # Hot reload configuration
├── Makefile            # Build commands
└── go.mod              # Go dependencies
\`\`\`

### Available Make Commands

\`\`\`bash
make dev          # Start with hot reload (Air)
make run-api      # Run without hot reload
make test         # Run tests
make clean        # Clean build artifacts
make deploy       # Deploy to GCP Cloud Run
\`\`\`

## 🚀 Deployment

### Deploy to Google Cloud Run

\`\`\`bash
make deploy
\`\`\`

Or manually:

\`\`\`bash
./deployments/deploy.sh
\`\`\`

## 🔐 Environment Variables

| Variable | Description | Required |
|----------|-------------|----------|
| \`GOOGLE_APPLICATION_CREDENTIALS\` | Path to GCP service account JSON | Yes |
| \`GCP_PROJECT_ID\` | Google Cloud Project ID | Yes |
| \`FIRESTORE_DATABASE_ID\` | Firestore database ID | No (default: "(default)") |
| \`GEMINI_API_KEY\` | Google Gemini API key | Yes |
| \`OPENROUTER_API_KEY\` | OpenRouter API key | Yes |
| \`NOTION_CLIENT_ID\` | Notion OAuth client ID | Yes |
| \`NOTION_CLIENT_SECRET\` | Notion OAuth secret | Yes |
| \`NOTION_REDIRECT_URI\` | OAuth redirect URI | Yes |
| \`PORT\` | Server port | No (default: 8080) |
| \`ENVIRONMENT\` | Environment (development/production) | No |

## 📖 API Documentation

See [docs/](./docs/) for detailed API documentation.

## 🤝 Related Projects

- [LingoFetch Extension](https://github.com/yourusername/lingofetch-extension) - Chrome extension frontend

## 📝 License

MIT License - see LICENSE file for details

## 🐛 Issues

Report issues at: https://github.com/yourusername/lingofetch-api/issues

