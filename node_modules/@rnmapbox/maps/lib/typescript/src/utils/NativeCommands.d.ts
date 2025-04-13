/// <reference types="react" />
export type NativeArg = string | number | boolean | null | {
    [k: string]: NativeArg;
} | NativeArg[] | Function | GeoJSON.Geometry | undefined;
type FunctionKeys<T> = keyof {
    [K in keyof T as T[K] extends Function ? K : never]: T[K];
};
type RefType = React.Component;
export declare class NativeCommands<Spec extends object> {
    module: Spec;
    preRefMethodQueue: Array<{
        method: {
            name: FunctionKeys<Spec>;
            args: NativeArg[];
        };
        resolver: (value: unknown) => void;
    }>;
    nativeRef: RefType | undefined;
    constructor(module: Spec);
    setNativeRef(nativeRef: RefType): Promise<void>;
    call<T>(name: FunctionKeys<Spec>, args: NativeArg[]): Promise<T>;
    _call<T>(name: FunctionKeys<Spec>, nativeRef: RefType, args: NativeArg[]): Promise<T>;
}
export {};
//# sourceMappingURL=NativeCommands.d.ts.map