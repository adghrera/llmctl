#!/usr/bin/env python3
"""Reference llmctl-stdio/1 backend.

Protocol (JSON lines over stdin/stdout):
  server -> client: {"protocol":"llmctl-stdio/1","name":...,"models":[...]}
  client -> server: {"id":"<uuid>","method":"chat.completions","params":{...openai body...}}
  server -> client: {"id":"<uuid>","type":"chunk","data":{...openai chunk...}}  (0..n times)
  server -> client: {"id":"<uuid>","type":"done","data":{...final chunk...}}
  errors:          {"id":"<uuid>","type":"error","error":{"message":...}}
  keepalive:       {"method":"ping"} -> {"type":"pong"}

If a llama-server binary is available (PATH or $LLMCTL_LLAMA_SERVER), it is
launched on a free localhost port and OpenAI requests are proxied to it —
giving real GGUF inference over stdio IPC. Otherwise the backend runs in
echo mode (deterministic canned responses) so the protocol can be exercised
anywhere.
"""
import argparse
import json
import os
import shutil
import socket
import subprocess
import sys
import threading
import time
import urllib.request


def log(msg):
    # stderr only — stdout is the protocol channel.
    sys.stderr.write("ref-stdio: %s\n" % msg)
    sys.stderr.flush()


def free_port():
    s = socket.socket()
    s.bind(("127.0.0.1", 0))
    p = s.getsockname()[1]
    s.close()
    return p


def find_llama_server():
    env = os.environ.get("LLMCTL_LLAMA_SERVER")
    if env and os.path.exists(env):
        return env
    return shutil.which("llama-server") or shutil.which("llama-server.exe")


class LlamaProxy:
    """Wraps a launched llama-server; converts its SSE stream to chunks."""

    def __init__(self, model, ctx):
        self.model = model
        self.port = free_port()
        self.base = "http://127.0.0.1:%d/v1" % self.port
        binpath = find_llama_server()
        if not binpath:
            raise RuntimeError("llama-server not found")
        log("launching %s on port %d" % (binpath, self.port))
        self.proc = subprocess.Popen(
            [binpath, "--model", model, "--host", "127.0.0.1",
             "--port", str(self.port), "--ctx-size", str(ctx),
             "--n-gpu-layers", "0"],
            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
        )
        deadline = time.time() + 120
        while time.time() < deadline:
            if self.proc.poll() is not None:
                raise RuntimeError("llama-server exited early")
            try:
                with urllib.request.urlopen(self.base[:-3] + "/health", timeout=2) as r:
                    if r.status == 200:
                        log("llama-server ready")
                        return
            except Exception:
                time.sleep(0.5)
        raise RuntimeError("llama-server did not become healthy in 120s")

    def close(self):
        try:
            self.proc.terminate()
            self.proc.wait(timeout=10)
        except Exception:
            try:
                self.proc.kill()
            except Exception:
                pass

    def chat(self, params, sink):
        stream = bool(params.get("stream"))
        body = dict(params)
        body["stream"] = stream
        req = urllib.request.Request(
            self.base + "/chat/completions",
            data=json.dumps(body).encode(),
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        with urllib.request.urlopen(req, timeout=600) as resp:
            if not stream:
                final = json.loads(resp.read().decode())
                sink("chunk", final)
                sink("done", final)
                return
            # SSE: data: {...} lines, terminated by data: [DONE]
            last = None
            for raw in resp:
                line = raw.decode(errors="replace").strip()
                if not line.startswith("data:"):
                    continue
                payload = line[5:].strip()
                if payload == "[DONE]":
                    break
                try:
                    chunk = json.loads(payload)
                except json.JSONDecodeError:
                    continue
                last = chunk
                sink("chunk", chunk)
            sink("done", last or {})


class EchoBackend:
    """Deterministic canned responses; proves the protocol without a model."""

    def __init__(self, model, ctx):
        self.model = model
        log("echo mode (no llama-server found)")

    def close(self):
        pass

    def chat(self, params, sink):
        user = ""
        for m in params.get("messages", []):
            if m.get("role") == "user":
                user = m.get("content", "")
        text = ("[llmctl reference stdio backend, echo mode] You said: %r"
                % (user[:200],))
        tokens = [text[i:i + 4] for i in range(0, len(text), 4)]
        if not params.get("stream"):
            final = {
                "id": "chatcmpl-echo", "object": "chat.completion",
                "created": int(time.time()), "model": self.model,
                "choices": [{"index": 0, "finish_reason": "stop",
                             "message": {"role": "assistant", "content": text}}],
                "usage": {"prompt_tokens": 1, "completion_tokens": len(tokens),
                          "total_tokens": len(tokens) + 1},
            }
            sink("chunk", final)
            sink("done", final)
            return
        cid = "chatcmpl-echo"
        for i, tok in enumerate(tokens):
            chunk = {
                "id": cid, "object": "chat.completion.chunk",
                "created": int(time.time()), "model": self.model,
                "choices": [{"index": 0,
                             "finish_reason": None if i < len(tokens) - 1 else "stop",
                             "delta": {"role": "assistant", "content": tok}}],
            }
            sink("chunk", chunk)
            time.sleep(0.01)
        sink("done", {})


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--model", required=True)
    ap.add_argument("--ctx", default="2048")
    args = ap.parse_args()

    try:
        engine = LlamaProxy(args.model, int(args.ctx))
        mode = "llama-server proxy"
    except Exception as e:
        log("proxy unavailable (%s); falling back to echo" % e)
        engine = EchoBackend(args.model, args.ctx)
        mode = "echo"

    hello = {"protocol": "llmctl-stdio/1", "name": "ref-stdio",
             "mode": mode, "model": args.model,
             "models": [args.model]}
    sys.stdout.write(json.dumps(hello) + "\n")
    sys.stdout.flush()

    write_lock = threading.Lock()

    def sink(rid, typ, data):
        with write_lock:
            sys.stdout.write(json.dumps({"id": rid, "type": typ, "data": data}) + "\n")
            sys.stdout.flush()

    def handle(req):
        rid = req.get("id", "")
        method = req.get("method", "")
        if method == "ping":
            with write_lock:
                sys.stdout.write(json.dumps({"id": rid, "type": "pong"}) + "\n")
                sys.stdout.flush()
            return
        if method != "chat.completions":
            sink(rid, "error", None)
            with write_lock:
                sys.stdout.write(json.dumps(
                    {"id": rid, "type": "error",
                     "error": {"message": "unknown method %r" % method}}) + "\n")
                sys.stdout.flush()
            return
        try:
            engine.chat(req.get("params", {}), lambda t, d: sink(rid, t, d))
        except Exception as e:
            with write_lock:
                sys.stdout.write(json.dumps(
                    {"id": rid, "type": "error",
                     "error": {"message": str(e)}}) + "\n")
                sys.stdout.flush()

    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        try:
            req = json.loads(line)
        except json.JSONDecodeError:
            continue
        t = threading.Thread(target=handle, args=(req,), daemon=True)
        t.start()

    engine.close()


if __name__ == "__main__":
    main()
