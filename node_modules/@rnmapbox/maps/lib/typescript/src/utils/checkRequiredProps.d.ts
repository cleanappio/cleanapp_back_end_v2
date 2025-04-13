type RequiredKeys<T> = {
    [k in keyof T]-?: undefined extends T[k] ? never : k;
}[keyof T];
export default function checkRequiredProps<Props extends {
    [key: string]: unknown;
}>(tag: string, props: Props, required: RequiredKeys<Props>[]): void;
export {};
//# sourceMappingURL=checkRequiredProps.d.ts.map