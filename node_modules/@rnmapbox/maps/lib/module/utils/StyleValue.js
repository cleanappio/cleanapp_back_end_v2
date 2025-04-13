"use strict";

import { Image, processColor } from 'react-native';
import BridgeValue from './BridgeValue';
import { getStyleType } from './styleMap';
export function transformStyle(style) {
  if (!style) {
    return;
  }
  const nativeStyle = {};
  const styleProps = Object.keys(style);
  for (const styleProp of styleProps) {
    const styleType = getStyleType(styleProp);
    let rawStyle = style[styleProp];
    if (styleType === 'color' && typeof rawStyle === 'string') {
      const color = processColor(rawStyle);
      if (color === null || color === undefined || typeof color === 'symbol') {
        console.error(`RNMapbox: Invalid color value: ${rawStyle} using red`);
        rawStyle = 'ff0000';
      } else {
        rawStyle = color;
      }
    } else if (styleType === 'image' && typeof rawStyle === 'number') {
      rawStyle = Image.resolveAssetSource(rawStyle) || {};
    }
    if (styleType === 'image' && !(Array.isArray(rawStyle) || typeof rawStyle === 'string' && !rawStyle.startsWith('http://') && !rawStyle.startsWith('https://'))) {
      console.warn(`RNMapbox: Image or URL in ${styleProp} is deprecated, use Images component. See https://github.com/rnmapbox/maps/wiki/Deprecated-URLInIconImages`);
    }
    if (rawStyle !== undefined) {
      const bridgeValue = new BridgeValue(rawStyle);
      nativeStyle[styleProp] = {
        styletype: styleType,
        stylevalue: bridgeValue.toJSON()
      };
    }
  }
  return nativeStyle;
}
//# sourceMappingURL=StyleValue.js.map