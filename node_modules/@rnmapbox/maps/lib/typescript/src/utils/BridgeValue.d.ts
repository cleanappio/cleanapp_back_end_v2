export type RawValueType = string | number | boolean | RawValueType[] | {
    [key: string]: RawValueType;
};
export type StyleValueJSON = {
    type: 'boolean';
    value: boolean;
} | {
    type: 'number';
    value: number;
} | {
    type: 'string';
    value: string;
} | {
    type: 'hashmap';
    value: object;
} | {
    type: 'array';
    value: unknown[];
};
type StyleValueTypes = 'boolean' | 'number' | 'string' | 'hashmap' | 'array';
export default class BridgeValue {
    rawValue: RawValueType;
    constructor(rawValue: RawValueType);
    get type(): StyleValueTypes;
    get value(): string | number | boolean | RawValueType[] | {
        [key: string]: RawValueType;
    } | ({
        type: "boolean";
        value: boolean;
    } | {
        type: "number";
        value: number;
    } | {
        type: "string";
        value: string;
    } | {
        type: "hashmap";
        value: object;
    } | {
        type: "array";
        value: unknown[];
    })[] | StyleValueJSON[][];
    toJSON(formatter?: <T>(arg0: T) => T): StyleValueJSON;
}
export {};
//# sourceMappingURL=BridgeValue.d.ts.map