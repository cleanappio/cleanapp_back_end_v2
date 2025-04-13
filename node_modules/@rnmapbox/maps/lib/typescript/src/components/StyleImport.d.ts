import React from 'react';
type Props = {
    /**
     * id of the style import (eg. basemap)
     */
    id: string;
    /**
     * existing is now always required as true
     */
    existing: boolean;
    /**
     * config is a dictionary of configuration options for the style import.
     *
     * See https://github.com/mapbox/mapbox-maps-ios/blob/main/Sources/MapboxMaps/Documentation.docc/Migrate%20to%20v11.md#21-the-mapbox-standard-style
     */
    config: {
        [key: string]: string;
    };
};
/**
 * Use StyleImport to set configuration options on the new standard style. **V11 only.**
 *
 * See https://github.com/mapbox/mapbox-maps-ios/blob/main/Sources/MapboxMaps/Documentation.docc/Migrate%20to%20v11.md#21-the-mapbox-standard-style
 */
declare const _default: React.MemoExoticComponent<(props: Props) => React.JSX.Element>;
export default _default;
//# sourceMappingURL=StyleImport.d.ts.map