"use strict";

import React from 'react';
import { NativeModules } from 'react-native';
import RNMBXHeatmapLayerNativeComponent from '../specs/RNMBXHeatmapLayerNativeComponent';
import AbstractLayer from './AbstractLayer';
import { jsx as _jsx } from "react/jsx-runtime";
const Mapbox = NativeModules.RNMBXModule;

// @{codepart-replace-start(LayerPropsCommon.codepart-tsx)}

// @{codepart-replace-end}

/**
 * HeatmapLayer is a style layer that renders one or more filled circles on the map.
 */
class HeatmapLayer extends AbstractLayer {
  static defaultProps = {
    sourceID: Mapbox.StyleSource.DefaultSourceID
  };
  render() {
    const props = {
      ...this.props,
      ...this.baseProps,
      sourceLayerID: this.props.sourceLayerID
    };
    return (
      /*#__PURE__*/
      // @ts-expect-error just codegen stuff
      _jsx(RNMBXHeatmapLayerNativeComponent, {
        ref: this.setNativeLayer,
        ...props
      })
    );
  }
}
export default HeatmapLayer;
//# sourceMappingURL=HeatmapLayer.js.map