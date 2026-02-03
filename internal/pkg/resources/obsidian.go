package resources

import (
	"fmt"
)

func GetObsidianConnectHTML(userID string) string {
	return fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <title>Connect Obsidian - LingoFetch</title>
    <style>
        body {
            font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
            background-color: #f9fafb;
            color: #1f2937;
            display: flex;
            align-items: center;
            justify-content: center;
            min-height: 100vh;
            margin: 0;
        }
        .card {
            background: white;
            padding: 2.5rem;
            border-radius: 1rem;
            box-shadow: 0 10px 25px -5px rgba(0, 0, 0, 0.1), 0 8px 10px -6px rgba(0, 0, 0, 0.1);
            max-width: 450px;
            width: 100%%;
        }
        h1 {
            color: #592171;
            font-size: 1.875rem;
            margin-bottom: 0.5rem;
            font-weight: 800;
            text-align: center;
        }
        p {
            color: #6b7280;
            margin-bottom: 2rem;
            text-align: center;
            font-size: 0.875rem;
        }
        .form-group {
            margin-bottom: 1.25rem;
        }
        label {
            display: block;
            font-size: 0.875rem;
            font-weight: 600;
            margin-bottom: 0.5rem;
            color: #374151;
        }
        input {
            width: 100%%;
            padding: 0.75rem;
            border: 1px solid #d1d5db;
            border-radius: 0.5rem;
            box-sizing: border-box;
            font-size: 0.875rem;
            transition: border-color 0.2s;
        }
        input:focus {
            outline: none;
            border-color: #592171;
            ring: 2px solid #592171;
        }
        button {
            width: 100%%;
            padding: 0.75rem;
            background-color: #592171;
            color: white;
            border: none;
            border-radius: 0.5rem;
            font-weight: 600;
            cursor: pointer;
            transition: background-color 0.2s;
            margin-top: 1rem;
        }
        button:hover {
            background-color: #431956;
        }
        .instruction {
            margin-top: 1.5rem;
            padding: 1rem;
            background-color: #f3f4f6;
            border-radius: 0.5rem;
            font-size: 0.75rem;
            color: #4b5563;
        }
        code {
            background: #e5e7eb;
            padding: 0.1rem 0.3rem;
            border-radius: 0.2rem;
        }
    </style>
</head>
<body>
    <div class="card">
        <h1>Connect Obsidian</h1>
        <p>Enter your Local REST API details to link LingoFetch to your vault.</p>
        
        <form id="obsidianForm">
            <input type="hidden" name="user_id" value="%s">
            
            <div class="form-group">
                <label for="base_url">Obsidian Base URL</label>
                <input type="text" id="base_url" name="base_url" value="http://127.0.0.1:27123" placeholder="http://127.0.0.1:27123" required>
            </div>
            
            <div class="form-group">
                <label for="access_token">API Access Token</label>
                <input type="password" id="access_token" name="access_token" placeholder="Your API Key" required>
            </div>
            
            <button type="submit">Connect Vault</button>
        </form>

        <div class="instruction">
            <strong>How to get this?</strong><br>
            1. Install <code>Local REST API</code> plugin in Obsidian.<br>
            2. Enable it and copy the API Key from the plugin settings.<br>
            3. Ensure the server is running (default port 27123).
        </div>
    </div>

    <script>
        document.getElementById('obsidianForm').addEventListener('submit', async (e) => {
            e.preventDefault();
            const formData = new FormData(e.target);
            const data = Object.fromEntries(formData.entries());
            
            const btn = e.target.querySelector('button');
            btn.textContent = 'Connecting...';
            btn.disabled = true;

            try {
                const resp = await fetch('/api/v1/user/obsidian/settings', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(data)
                });
                
                if (resp.ok) {
                    window.location.href = '/api/v1/user/obsidian/status?user_id=' + data.user_id + '&success=true';
                } else {
                    alert('Failed to connect. Please check your URL and Token.');
                    btn.textContent = 'Connect Vault';
                    btn.disabled = false;
                }
            } catch (err) {
                alert('Error: ' + err.message);
                btn.textContent = 'Connect Vault';
                btn.disabled = false;
            }
        });
    </script>
</body>
</html>
`, userID)
}
