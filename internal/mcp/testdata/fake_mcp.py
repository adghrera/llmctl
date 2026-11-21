import sys, json
for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    try:
        req = json.loads(line)
    except Exception:
        continue
    rid = req.get("id")
    if rid is None:  # notification
        continue
    m = req.get("method")
    if m == "initialize":
        resp = {"jsonrpc": "2.0", "id": rid, "result": {
            "protocolVersion": "2024-11-05",
            "capabilities": {"tools": {}},
            "serverInfo": {"name": "fake", "version": "1"}}}
    elif m == "tools/list":
        resp = {"jsonrpc": "2.0", "id": rid, "result": {"tools": [
            {"name": "echo", "description": "Echo back",
             "inputSchema": {"type": "object", "properties": {"msg": {"type": "string"}}}},
            {"name": "boom", "description": "Always errors"}]}}
    elif m == "tools/call":
        params = req.get("params", {})
        if params.get("name") == "boom":
            resp = {"jsonrpc": "2.0", "id": rid, "result": {
                "isError": True, "content": [{"type": "text", "text": "kaboom"}]}}
        else:
            args = params.get("arguments") or {}
            resp = {"jsonrpc": "2.0", "id": rid, "result": {
                "content": [{"type": "text", "text": str(args.get("msg", ""))}]}}
    else:
        resp = {"jsonrpc": "2.0", "id": rid,
                "error": {"code": -32601, "message": "method not found"}}
    sys.stdout.write(json.dumps(resp) + "\n")
    sys.stdout.flush()
