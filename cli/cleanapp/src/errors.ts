export const EXIT = {
  OK: 0,
  USER: 1,
  NET: 2,
} as const;

export class CLIError extends Error {
  public readonly exitCode: number;
  public readonly status?: number;

  constructor(message: string, exitCode: number, status?: number) {
    super(message);
    this.name = "CLIError";
    this.exitCode = exitCode;
    this.status = status;
  }
}
