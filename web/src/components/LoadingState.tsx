import { LoaderCircle } from "lucide-react";

type LoadingStateProps = {
  label?: string;
};

export function LoadingState({ label = "正在加载" }: LoadingStateProps) {
  return (
    <div className="loading-state" role="status" aria-live="polite">
      <LoaderCircle className="loading-state-icon" size={20} aria-hidden="true" />
      <span>{label}</span>
    </div>
  );
}
