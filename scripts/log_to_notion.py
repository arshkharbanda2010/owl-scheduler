import os, json, urllib.request

# Read API key
env_path = os.path.join(os.path.expanduser("~"), "AppData", "Local", "hermes", ".env")
api_key = ""
with open(env_path) as f:
    for line in f:
        if line.strip().startswith("NOTION_API_KEY="):
            api_key = line.strip().split("=", 1)[1]
            break

# Create page in Work Logbook database
payload = json.dumps({
    "parent": {"database_id": "36af8d8e-ef55-81ce-96f9-fb169205d06a"},
    "properties": {
        "Title": {"title": [{"text": {"content": "Built Custom Kubernetes Scheduler (owl-scheduler)"}}]},
        "Status": {"select": {"name": "Done"}},
        "Date": {"date": {"start": "2026-06-18"}},
        "Summary": {"rich_text": [{"text": {"content": "Built a fully custom Kubernetes scheduler in Go. Features: filter-score-bind pipeline (4 filters: NodeResourcesFit, NodeName, NodeUnschedulable, TaintToleration; 3 scorers: LeastRequestedPriority, BalancedResourceAllocation, NodeAffinity), heap-based priority queue, preemption with PDB awareness, binder with retry+backoff. 29 files: Go source, K8s manifests (Deployment, RBAC, ConfigMap, Service, PriorityClasses), Helm chart, Dockerfile (distroless), build scripts, test workloads."}}]},
        "Tags": {"multi_select": [{"name": "k8s"}, {"name": "scheduler"}, {"name": "go"}, {"name": "devops"}]},
        "Priority": {"select": {"name": "High"}},
        "Category": {"select": {"name": "Development"}}
    }
}).encode()

req = urllib.request.Request(
    "https://api.notion.com/v1/pages",
    data=payload,
    method="POST",
    headers={
        "Authorization": f"Bearer {api_key}",
        "Notion-Version": "2022-06-28",
        "Content-Type": "application/json"
    }
)

try:
    with urllib.request.urlopen(req) as resp:
        data = json.loads(resp.read())
    page_id = data["id"]
    page_url = data.get("url", "")
    print(f"SUCCESS! Page ID: {page_id}")
    print(f"URL: {page_url}")
except urllib.error.HTTPError as e:
    print(f"Error {e.code}: {e.read().decode()[:500]}")
