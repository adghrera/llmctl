import { NavLink, Route, Routes } from "react-router-dom";
import { useStore } from "./state/store";
import Overview from "./pages/Overview";
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
import Usage from "./pages/Usage";
import Keys from "./pages/Keys";
import SettingsPage from "./pages/Settings";
import { Toasts } from "./ui/toast";

const NAV = [
  { to: "/", label: "Overview", end: true },
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
  { to: "/cost", label: "Cost" },
  { to: "/usage", label: "Usage" },
  { to: "/keys", label: "API Keys" },
  { to: "/settings", label: "Settings" },
];

export default function App() {
  const { status } = useStore();

  return (
    <>
      <header>
        <div className="logo">
          llm<span>ctl</span>
        </div>
        <div className="gw">{status ? `gateway ${status.gateway}` : ""}</div>
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
          <Route path="/usage" element={<Usage />} />
          <Route path="/keys" element={<Keys />} />
          <Route path="/settings" element={<SettingsPage />} />
        </Routes>
      </main>
      <Toasts />
    </>
  );
}
