type Props = { className?: string };

export default function Logo({ className = "w-6 h-6" }: Props) {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      viewBox="0 0 400 400"
      fill="none"
      strokeLinecap="round"
      strokeLinejoin="round"
      className={className}
    >
      <g transform="rotate(15, 200, 200)">
        <path d="M 33 200 A 167 167 0 0 1 200 33" stroke="#9C27B0" strokeWidth="40" fill="none" />
        <path d="M 367 200 A 167 167 0 0 1 200 367" stroke="#9C27B0" strokeWidth="40" fill="none" />
        <path d="M 300 200 A 100 100 0 0 1 200 300" stroke="#00C853" strokeWidth="40" fill="none" />
        <path
          d="M 100 201.65 A 100 100 0 0 1 199.683 100"
          stroke="#00C853"
          strokeWidth="40"
          fill="none"
        />
        <circle cx="200" cy="200" r="45" fill="#FF6B35" />
      </g>
    </svg>
  );
}
