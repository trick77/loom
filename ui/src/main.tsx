import React from "react";
import ReactDOM from "react-dom/client";
import App from "./App";
import { SharePage } from "./share/SharePage";
import "./index.css";

const root = ReactDOM.createRoot(document.getElementById("root")!);

// Public share viewer: handled BEFORE <App/> so it never runs the auth gate
// (App calls getMe and redirects to sign-in). A logged-out visitor with a share
// link must reach a standalone, read-only page with no app shell.
const sharePath = "/share/";
if (window.location.pathname.startsWith(sharePath)) {
  const shareId = decodeURIComponent(window.location.pathname.slice(sharePath.length));
  root.render(
    <React.StrictMode>
      <SharePage shareId={shareId} />
    </React.StrictMode>,
  );
} else {
  root.render(
    <React.StrictMode>
      <App />
    </React.StrictMode>,
  );
}
