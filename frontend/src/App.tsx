import { useState } from "react";
import { NavLink, Route, Routes } from "react-router-dom";
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

const NAV = [
  { to: "/", label: "Overview", end: true },
  { to: "/search", label: "Search" },
  { to: "/backends", label: "Backends" },
  { to: "/models", label: "Models" },
  { to: "/instances", label: "Instances" },
  { to: "/chat", label: "Chat" },
  { to: "/tools", label: "Tools" },
  { to: "/skills", label: "Skills" },
  { to: "/memory", label: "Memory" },
  { to: "/compare", label: "Compare" },
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
  const [light, setLight] = useState(() => document.documentElement.dataset.theme === "light");

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
          title={light ? "Switch to dark theme" : "Switch to light theme"}
          onClick={toggleTheme}
        >
          {light ? "🌙" : "☀️"}
        </button>
      </header>
      <nav>
        {NAV.map((n) => (
          <NavLink key={n.to} to={n.to} end={n.end} className={({ isActive }) => (isActive ? "active" : "")}>
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
      <Toasts />
    </>
  );
}
