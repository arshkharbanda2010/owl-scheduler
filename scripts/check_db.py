import os, json, urllib.request

# Read API key from .env
env_path = os.path.join(os.path.expanduser("~"), "AppData", "Local", "hermes", ".env")
api_key = ""
with open(env_path) as f:
    for line in f:
        if line.strip().startswith("NOTION_API_KEY="):
            api_key = line.strip().split("=", 1)[1]
            break

print(f"Key length: {len(api_key)}, starts with: {api_key[:8]}")

# First, query the database to get property names
req = urllib.request.Request(
    "https://api.notion.com/v1/databases/36af8d8e-ef55-81ce-96f9-fb169205d06a",
    method="GET",
    headers={
        "Authorization": f"Bearer {api_key}",
        "Notion-Version": "2022-06-28"
    }
)

try:
    with urllib.request.urlopen(req) as resp:
        db = json.loads(resp.read())
    print("\nDatabase properties:")
    for name, prop in db.get("properties", {}).items():
        print(f"  {name:20s} type={prop['type']:12s}")
except urllib.error.HTTPError as e:
    print(f"DB query error {e.code}: {e.read().decode()[:300]}")
