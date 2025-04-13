"use strict";

Object.defineProperty(exports, "__esModule", {
  value: true
});
exports.transformStyle = transformStyle;
var _reactNative = require("react-native");
var _BridgeValue = _interopRequireDefault(require("./BridgeValue"));
var _styleMap = require("./styleMap");
function _interopRequireDefault(e) { return e && e.__esModule ? e : { default: e }; }
function transformStyle(style) {
  if (!style) {
    return;
  }
  const nativeStyle = {};
  const styleProps = Object.keys(style);
  for (const styleProp of styleProps) {
    const styleType = (0, _styleMap.getStyleType)(styleProp);
    let rawStyle = style[styleProp];
    if (styleType === 'color' && typeof rawStyle === 'string') {
      const color = (0, _reactNative.processColor)(rawStyle);
      if (color === null || color === undefined || typeof color === 'symbol') {
        console.error(`RNMapbox: Invalid color value: ${rawStyle} using red`);
        rawStyle = 'ff0000';
      } else {
        rawStyle = color;
      }
    } else if (styleType === 'image' && typeof rawStyle === 'number') {
      rawStyle = _reactNative.Image.resolveAssetSource(rawStyle) || {};
    }
    if (styleType === 'image' && !(Array.isArray(rawStyle) || typeof rawStyle === 'string' && !rawStyle.startsWith('http://') && !rawStyle.startsWith('https://'))) {
      console.warn(`RNMapbox: Image or URL in ${styleProp} is deprecated, use Images component. See https://github.com/rnmapbox/maps/wiki/Deprecated-URLInIconImages`);
    }
    if (rawStyle !== undefined) {
      const bridgeValue = new _BridgeValue.default(rawStyle);
      nativeStyle[styleProp] = {
        styletype: styleType,
        stylevalue: bridgeValue.toJSON()
      };
    }
  }
  return nativeStyle;
}
//# sourceMappingURL=StyleValue.js.map