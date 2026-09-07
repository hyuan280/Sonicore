import { useRef, useState, type ChangeEvent } from "react";
import { useTranslation } from "react-i18next";
import { useAuth } from "../stores/auth";
import { translateApiError } from "../i18n/errorCodes";
import { Button } from "../components/ui/button";
import { Input } from "../components/ui/input";
import { Card } from "../components/ui/card";
import { api } from "../api/client";
import UserAvatar from "../components/UserAvatar";
import { clearAvatarCache } from "../lib/avatarCache";
import { Loader2, Upload, RotateCcw, UserRound, KeyRound } from "lucide-react";

const MAX_AVATAR_SIZE = 1 * 1024 * 1024;
const AVATAR_FORMATS = ["image/jpeg", "image/png", "image/webp"];

export default function ProfilePage() {
  const { t } = useTranslation();
  const { user, logout, setUser } = useAuth();
  const fileInputRef = useRef<HTMLInputElement>(null);
  const [avatarError, setAvatarError] = useState("");
  const [uploading, setUploading] = useState(false);
  const [removing, setRemoving] = useState(false);

  const [pwForm, setPwForm] = useState({ oldPw: "", newPw: "", confirmPw: "" });
  const [pwError, setPwError] = useState("");
  const [pwSuccess, setPwSuccess] = useState("");
  const [pwSaving, setPwSaving] = useState(false);

  if (!user) return null;

  const roleLabels: Record<string, string> = {
    super_admin: t("settings.superAdmin"),
    admin: t("settings.admin"),
    user: t("settings.user"),
  };
  const roleLabel = roleLabels[user.role] || t("settings.user");

  const pickFile = () => fileInputRef.current?.click();

  const onFileSelected = async (e: ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    e.target.value = "";
    if (!file) return;
    setAvatarError("");
    if (file.size > MAX_AVATAR_SIZE) {
      setAvatarError(t("errors.user.AVATAR_TOO_LARGE"));
      return;
    }
    if (!AVATAR_FORMATS.includes(file.type)) {
      setAvatarError(t("errors.user.AVATAR_INVALID_FORMAT"));
      return;
    }
    setUploading(true);
    try {
      const res = await api.user.updateAvatar(file);
      setUser({ avatar_format: res.avatar_format });
      clearAvatarCache();
    } catch (err) {
      setAvatarError(translateApiError(t, err));
    }
    setUploading(false);
  };

  const removeAvatar = async () => {
    setAvatarError("");
    setRemoving(true);
    try {
      await api.user.updateAvatar(new Blob([]));
      setUser({ avatar_format: "" });
      clearAvatarCache();
    } catch (err) {
      setAvatarError(translateApiError(t, err));
    }
    setRemoving(false);
  };

  const changePassword = async () => {
    if (pwSaving) return;
    setPwSaving(true);
    setPwError("");
    setPwSuccess("");
    if (!pwForm.oldPw || !pwForm.newPw || !pwForm.confirmPw) {
      setPwError(t("profile.fillAllFields"));
      setPwSaving(false);
      return;
    }
    if (pwForm.newPw !== pwForm.confirmPw) {
      setPwError(t("settings.passwordsNoMatch"));
      setPwSaving(false);
      return;
    }
    try {
      await api.auth.changePassword(pwForm.oldPw, pwForm.newPw);
      setPwForm({ oldPw: "", newPw: "", confirmPw: "" });
      setPwSuccess(t("profile.passwordChanged"));
    } catch (err) {
      setPwError(translateApiError(t, err));
    }
    setPwSaving(false);
  };

  return (
    <div className="p-6 space-y-6">
      <h1 className="text-2xl font-bold">{t("profile.title")}</h1>

      <Card className="space-y-4">
        <h3 className="font-medium flex items-center gap-2">
          <UserRound className="w-4 h-4" /> {t("settings.account")}
        </h3>
        <div className="flex items-start gap-5">
          <UserAvatar avatarFormat={user.avatar_format} className="w-20 h-20 rounded-xl" />
          <div className="flex-1 min-w-0 space-y-3">
            <input
              ref={fileInputRef}
              type="file"
              accept="image/jpeg,image/png,image/webp"
              className="hidden"
              onChange={onFileSelected}
            />
            <div className="flex flex-wrap items-center gap-2">
              <Button
                variant="primary"
                size="sm"
                onClick={pickFile}
                disabled={uploading || removing}
              >
                {uploading ? (
                  <Loader2 className="w-4 h-4 animate-spin" />
                ) : (
                  <Upload className="w-4 h-4 mr-1" />
                )}
                {t("profile.uploadAvatar")}
              </Button>
              <Button
                variant="ghost"
                size="sm"
                onClick={removeAvatar}
                disabled={removing || uploading || !user.avatar_format}
                className="bg-red-500/10 text-red-400 hover:bg-red-500/20 hover:text-red-300"
              >
                {removing ? (
                  <Loader2 className="w-4 h-4 animate-spin mr-1" />
                ) : (
                  <RotateCcw className="w-4 h-4 mr-1" />
                )}
                {t("profile.resetAvatar")}
              </Button>
            </div>
            <p className="text-xs text-zinc-500">{t("profile.avatarHint")}</p>
            {avatarError && <p className="text-xs text-red-400">{avatarError}</p>}
          </div>
        </div>
        <div className="space-y-2 text-base">
          <p className="text-zinc-300">
            {t("settings.username")}:{" "}
            <span className="text-white font-medium">{user.username}</span>
          </p>
          <p className="text-zinc-300">
            {t("settings.email")}: <span className="text-white font-medium">{user.email}</span>
          </p>
          <p className="text-zinc-300">
            {t("settings.role")}: <span className="text-green-500 font-medium">{roleLabel}</span>
          </p>
        </div>
      </Card>

      <Card className="space-y-3">
        <h3 className="font-medium flex items-center gap-2">
          <KeyRound className="w-4 h-4" /> {t("settings.changePassword")}
        </h3>
        <div className="space-y-3 max-w-md">
          <Input
            type="password"
            placeholder={t("settings.currentPassword")}
            value={pwForm.oldPw}
            onChange={(e) => setPwForm({ ...pwForm, oldPw: e.target.value })}
          />
          <Input
            type="password"
            placeholder={t("settings.newPassword")}
            value={pwForm.newPw}
            onChange={(e) => setPwForm({ ...pwForm, newPw: e.target.value })}
          />
          <Input
            type="password"
            placeholder={t("settings.confirmPassword")}
            value={pwForm.confirmPw}
            onChange={(e) => setPwForm({ ...pwForm, confirmPw: e.target.value })}
            onKeyDown={(e) => e.key === "Enter" && changePassword()}
          />
        </div>
        {pwError && <p className="text-sm text-red-400">{pwError}</p>}
        {pwSuccess && <p className="text-sm text-green-400">{pwSuccess}</p>}
        <div className="flex justify-start">
          <Button
            variant="primary"
            size="sm"
            onClick={changePassword}
            disabled={pwSaving || !pwForm.oldPw || !pwForm.newPw || !pwForm.confirmPw}
          >
            {pwSaving ? <Loader2 className="w-4 h-4 animate-spin" /> : t("settings.update")}
          </Button>
        </div>
      </Card>

      <Button variant="danger" onClick={logout}>
        {t("settings.signOut")}
      </Button>
    </div>
  );
}
