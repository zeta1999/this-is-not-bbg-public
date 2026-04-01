import React, { useState, useCallback } from "react";
import { colors, fonts } from "../styles/theme";

interface Props {
  visible: boolean;
  onClose: () => void;
}

export const PairModal: React.FC<Props> = ({ visible, onClose }) => {
  const [token, setToken] = useState("");
  const [qrSrc, setQrSrc] = useState("");
  const [status, setStatus] = useState("Click 'Generate' to create a phone pairing token.");
  const [copied, setCopied] = useState(false);

  const generate = useCallback(async () => {
    setStatus("Generating...");
    try {
      const params = new URLSearchParams(window.location.search);
      const desktopToken = params.get("token") || "";
      const resp = await fetch(`http://localhost:9474/api/v1/pair/phone?token=${desktopToken}`, {
        method: "POST",
      });
      if (!resp.ok) {
        setStatus(`Error: ${resp.status} ${await resp.text()}`);
        return;
      }
      const data = await resp.json();
      setToken(data.token);
      if (data.qr) {
        setQrSrc(`data:image/png;base64,${data.qr}`);
      }
      setStatus("Token generated. Paste in phone Settings, or scan QR with camera.");
    } catch (e: any) {
      setStatus(`Failed: ${e.message}`);
    }
  }, []);

  const copyToken = useCallback(() => {
    navigator.clipboard.writeText(token);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  }, [token]);

  if (!visible) return null;

  return (
    <div style={s.overlay} onClick={onClose}>
      <div style={s.modal} onClick={(e) => e.stopPropagation()}>
        <div style={s.header}>
          <span style={s.title}>PHONE PAIRING</span>
          <button style={s.closeBtn} onClick={onClose}>✕</button>
        </div>

        <div style={s.body}>
          <p style={s.hint}>Generate a session token for the phone app. Each click creates a fresh token.</p>

          <button style={s.genBtn} onClick={generate}>GENERATE NEW TOKEN</button>

          {token && (
            <>
              <div style={s.tokenBox}>
                <code style={s.tokenText}>{token}</code>
                <button style={s.copyBtn} onClick={copyToken}>{copied ? "COPIED" : "COPY"}</button>
              </div>

              {qrSrc && (
                <div style={s.qrBox}>
                  <img src={qrSrc} alt="QR Code" style={s.qrImg} />
                  <p style={s.qrHint}>Scan with phone camera to auto-pair</p>
                </div>
              )}
            </>
          )}

          <p style={s.status}>{status}</p>
        </div>
      </div>
    </div>
  );
};

const s: Record<string, React.CSSProperties> = {
  overlay: { position: "fixed", inset: 0, background: "rgba(0,0,0,0.8)", display: "flex", alignItems: "center", justifyContent: "center", zIndex: 1000 },
  modal: { background: colors.bg, border: `1px solid ${colors.border}`, borderRadius: 8, width: 420, maxHeight: "80vh", overflow: "auto" },
  header: { display: "flex", justifyContent: "space-between", alignItems: "center", padding: "12px 16px", borderBottom: `1px solid ${colors.border}` },
  title: { fontSize: 13, fontWeight: 700, color: colors.amber, letterSpacing: "0.1em", fontFamily: fonts.mono },
  closeBtn: { background: "none", border: "none", color: colors.dimText, fontSize: 18, cursor: "pointer", padding: 4 },
  body: { padding: 16 },
  hint: { fontSize: 11, color: colors.dimText, fontFamily: fonts.mono, margin: "0 0 12px" },
  genBtn: { fontFamily: fonts.mono, fontSize: 12, fontWeight: 700, color: "#000", background: colors.amber, border: "none", borderRadius: 4, padding: "8px 20px", cursor: "pointer", marginBottom: 12 },
  tokenBox: { display: "flex", alignItems: "center", gap: 8, background: "#111", border: `1px solid ${colors.border}`, borderRadius: 4, padding: "8px 12px", marginBottom: 12 },
  tokenText: { fontSize: 10, color: colors.green, fontFamily: fonts.mono, wordBreak: "break-all" as any, flex: 1 },
  copyBtn: { fontFamily: fonts.mono, fontSize: 10, fontWeight: 700, color: colors.amber, background: "none", border: `1px solid ${colors.amber}`, borderRadius: 3, padding: "4px 10px", cursor: "pointer", flexShrink: 0 },
  qrBox: { textAlign: "center" as any, marginBottom: 12 },
  qrImg: { width: 200, height: 200, imageRendering: "pixelated" as any },
  qrHint: { fontSize: 10, color: colors.dimText, fontFamily: fonts.mono, marginTop: 6 },
  status: { fontSize: 11, color: colors.dimText, fontFamily: fonts.mono, margin: 0, borderTop: `1px solid ${colors.border}`, paddingTop: 8 },
};
