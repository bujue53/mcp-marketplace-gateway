import { Link, NavLink, useNavigate } from 'react-router-dom';
import { useAuth } from '../context/AuthContext';

export function Navbar() {
  const { user, token, logout } = useAuth();
  const navigate = useNavigate();

  const link = ({ isActive }: { isActive: boolean }) =>
    `px-3 py-1.5 rounded-lg text-sm transition ${isActive ? 'bg-brand-600 text-white' : 'text-slate-300 hover:bg-slate-800'}`;

  return (
    <header className="sticky top-0 z-20 border-b border-slate-800 bg-slate-950/80 backdrop-blur">
      <div className="mx-auto flex max-w-6xl items-center justify-between px-4 py-3">
        <Link to="/" className="flex items-center gap-2 text-lg font-bold text-white">
          <span className="inline-block h-3 w-3 rounded-full bg-brand-500" />
          MCP 工具市场
        </Link>
        <nav className="flex items-center gap-1">
          <NavLink to="/" className={link} end>
            市场
          </NavLink>
          <NavLink to="/dashboard" className={link}>
            统计
          </NavLink>
          {token ? (
            <button
              onClick={() => {
                logout();
                navigate('/login');
              }}
              className="ml-2 rounded-lg bg-slate-800 px-3 py-1.5 text-sm text-slate-200 hover:bg-slate-700"
            >
              {user?.username} · 退出
            </button>
          ) : (
            <NavLink to="/login" className={link}>
              登录
            </NavLink>
          )}
        </nav>
      </div>
    </header>
  );
}

