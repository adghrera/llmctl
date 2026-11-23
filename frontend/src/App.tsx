import { useEffect, useState } from "react";
import { NavLink, Route, Routes, useNavigate } from "react-router-dom";
import { useStore } from "./state/store";
import Overview from "./pages/Overview";
import Search from "./pages/Search";
import Backends from "./pages/Backends";
import Models from "./pages/Models";
import Instances from "./pages/Instances";
import Chat from "./pages/Chat";
import Tools from "./pages/Tools";
import Skills from "./pages/Skills";
import Memory from "./pages/Memory";
import Compare from "./pages/Compare";
import Bench from "./pages/Bench";
import Prompts from "./pages/Prompts";
import Files from "./pages/Files";
import Cost from "./pages/Cost";
import Scheduler from "./pages/Scheduler";
import KnowledgeBases from "./pages/KnowledgeBases";
import MCP from "./pages/MCP";
import Structured from "./pages/Structured";
import Requests from "./pages/Requests";
import Batch from "./pages/Batch";
import Usage from "./pages/Usage";
import Keys from "./pages/Keys";
import SettingsPage from "./pages/Settings";
import { Toasts } from "./ui/toast";

const NAV: { to: string; label: string; end?: boolean; title?: string }[] = [
  { to: "/", label: "Overview", end: true },
  { to: "/search", label: "Search", title: "Ctrl+K" },
  { to: "/backends", label: "Backends" },
  { to: "/models", label: "Models" },
  { to: "/instances", label: "Instances" },
  { to: "/chat", label: "Chat" },
  { to: "/tools", label: "Tools" },
  { to: "/skills", label: "Skills" },
  { to: "/memory", label: "Memory" },
  { to: "/compare", label: "Compare" },
  { to: "/bench", label: "Bench" },
  { to: "/prompts", label: "Prompts" },
  { to: "/files", label: "Files" },
  { to: "/knowledge-bases", label: "Knowledge Bases" },
  { to: "/mcp", label: "MCP" },
  { to: "/structured", label: "Structured" },
  { to: "/cost", label: "Cost" },
  { to: "/scheduler", label: "Scheduler" },
  { to: "/usage", label: "Usage" },
  { to: "/requests", label: "Requests" },
  { to: "/batch", label: "Batch" },
  { to: "/keys", label: "API Keys" },
  { to: "/settings", label: "Settings" },
];

export default function App() {
  const { status } = useStore();
  const navigate = useNavigate();
  const [light, setLight] = useState(() => document.documentElement.dataset.theme === "light");
  const [helpOpen, setHelpOpen] = useState(false);

  // Ctrl+K / Cmd+K jumps to the Search page from anywhere (F439).
  // ? opens the keyboard-shortcut help overlay (F514); Esc closes it.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && !e.shiftKey && e.key.toLowerCase() === "k") {
        e.preventDefault();
        navigate("/search");
        return;
      }
      // Alt+1…9 jumps to the first nine pages in sidebar order (F822).
      if (e.altKey && !e.ctrlKey && !e.metaKey && /^[1-9]$/.test(e.key)) {
        const pages = ["/", "/search", "/backends", "/models", "/instances", "/chat", "/tools", "/skills", "/memory"];
        e.preventDefault();
        navigate(pages[Number(e.key) - 1]);
        return;
      }
      if (e.key === "Escape") {
        setHelpOpen(false);
        return;
      }
      if (e.key !== "?" || e.ctrlKey || e.metaKey || e.altKey) return;
      const t = e.target as HTMLElement | null;
      const tag = t?.tagName ?? "";
      if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || t?.isContentEditable) return;
      e.preventDefault();
      setHelpOpen((v) => !v);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [navigate]);

  const toggleTheme = () => {
    const next = !light;
    setLight(next);
    if (next) document.documentElement.dataset.theme = "light";
    else delete document.documentElement.dataset.theme;
    try {
      localStorage.setItem("llmctl.theme", next ? "light" : "dark");
    } catch (e) { /* private mode */ }
  };

  return (
    <>
      <header>
        <div className="logo">
          llm<span>ctl</span>
        </div>
        <div className="gw">{status ? `gateway ${status.gateway}` : ""}</div>
        <button
          className="btn sm"
          style={{ marginLeft: 12 }}
          title="Keyboard shortcuts (?)"
          onClick={() => setHelpOpen((v) => !v)}
        >
          ?
        </button>
        <button
          className="btn sm"
          style={{ marginLeft: 12 }}
          title={light ? "Switch to dark theme" : "Switch to light theme"}
          onClick={toggleTheme}
        >
          {light ? "🌙" : "☀️"}
        </button>
      </header>
      <nav>
        {NAV.map((n) => (
          <NavLink key={n.to} to={n.to} end={n.end} title={n.title} className={({ isActive }) => (isActive ? "active" : "")}>
            {n.label}
          </NavLink>
        ))}
      </nav>
      <main>
        <Routes>
          <Route path="/" element={<Overview />} />
          <Route path="/search" element={<Search />} />
          <Route path="/backends" element={<Backends />} />
          <Route path="/models" element={<Models />} />
          <Route path="/instances" element={<Instances />} />
          <Route path="/chat" element={<Chat />} />
          <Route path="/tools" element={<Tools />} />
          <Route path="/skills" element={<Skills />} />
          <Route path="/memory" element={<Memory />} />
          <Route path="/compare" element={<Compare />} />
          <Route path="/bench" element={<Bench />} />
          <Route path="/prompts" element={<Prompts />} />
          <Route path="/files" element={<Files />} />
          <Route path="/cost" element={<Cost />} />
          <Route path="/scheduler" element={<Scheduler />} />
          <Route path="/knowledge-bases" element={<KnowledgeBases />} />
          <Route path="/mcp" element={<MCP />} />
          <Route path="/structured" element={<Structured />} />
          <Route path="/requests" element={<Requests />} />
          <Route path="/batch" element={<Batch />} />
          <Route path="/usage" element={<Usage />} />
          <Route path="/keys" element={<Keys />} />
          <Route path="/settings" element={<SettingsPage />} />
        </Routes>
      </main>
      {helpOpen && <ShortcutHelp onClose={() => setHelpOpen(false)} />}
      <Toasts />
    </>
  );
}

// ShortcutHelp lists the global keyboard shortcuts (F514).
const SHORTCUTS: { keys: string; what: string }[] = [
  { keys: "Ctrl/⌘ + K", what: "Jump to Search" },
  { keys: "Alt + 1…9", what: "Jump to pages: Overview, Search, Backends, Models, Instances, Chat, Tools, Skills, Memory" },
  { keys: "?", what: "Toggle this help" },
  { keys: "Esc", what: "Close dialogs and this help" },
  { keys: "Enter", what: "Send the message (Chat); run the current action in input fields" },
  { keys: "Shift + Enter", what: "New line in the Chat input" },
];

function ShortcutHelp({ onClose }: { onClose: () => void }) {
  return (
    <div className="confirm-backdrop" onClick={onClose}>
      <div className="confirm-modal" onClick={(e) => e.stopPropagation()}>
        <div className="confirm-header">
          <h3>Keyboard shortcuts</h3>
        </div>
        <div className="confirm-body">
          <table style={{ width: "100%", borderCollapse: "collapse" }}>
            <tbody>
              {SHORTCUTS.map((s) => (
                <tr key={s.keys}>
                  <td style={{ padding: "4px 12px 4px 0", whiteSpace: "nowrap", fontFamily: "var(--mono, monospace)" }}>
                    <code>{s.keys}</code>
                  </td>
                  <td style={{ padding: "4px 0" }}>{s.what}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <div className="confirm-actions">
          <button className="btn sm" onClick={onClose}>
            Close
          </button>
        </div>
      </div>
    </div>
  );
}
