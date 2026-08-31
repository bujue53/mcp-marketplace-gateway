export function StarRating({
  value,
  count,
  onRate,
}: {
  value: number;
  count?: number;
  onRate?: (rating: number) => void;
}) {
  const rounded = Math.round(value);
  return (
    <div className="flex items-center gap-1">
      <div className="flex">
        {[1, 2, 3, 4, 5].map((star) => (
          <button
            key={star}
            disabled={!onRate}
            onClick={() => onRate?.(star)}
            className={`text-sm ${star <= rounded ? 'text-amber-400' : 'text-slate-600'} ${onRate ? 'cursor-pointer' : 'cursor-default'}`}
            aria-label={`${star} 星`}
          >
            ★
          </button>
        ))}
      </div>
      <span className="text-xs text-slate-400">
        {value.toFixed(1)}
        {typeof count === 'number' && ` (${count})`}
      </span>
    </div>
  );
}

