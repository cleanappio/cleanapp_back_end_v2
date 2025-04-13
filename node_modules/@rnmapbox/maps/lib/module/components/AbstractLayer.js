"use strict";

import React from 'react';
import { processColor } from 'react-native';
import { getFilter } from '../utils/filterUtils';
import { transformStyle } from '../utils/StyleValue';
class AbstractLayer extends React.PureComponent {
  get baseProps() {
    return {
      ...this.props,
      id: this.props.id,
      existing: this.props.existing,
      sourceID: this.props.sourceID,
      reactStyle: this.getStyle(this.props.style),
      minZoomLevel: this.props.minZoomLevel,
      maxZoomLevel: this.props.maxZoomLevel,
      aboveLayerID: this.props.aboveLayerID,
      belowLayerID: this.props.belowLayerID,
      layerIndex: this.props.layerIndex,
      filter: getFilter(this.props.filter),
      style: undefined
    };
  }
  nativeLayer = null;
  setNativeLayer = instance => {
    this.nativeLayer = instance;
  };
  getStyleTypeFormatter(styleType) {
    if (styleType === 'color') {
      return processColor;
    }
    return undefined;
  }
  getStyle(style) {
    return transformStyle(style);
  }
  setNativeProps(props) {
    if (this.nativeLayer) {
      let propsToPass = props;
      if (props.style) {
        propsToPass = {
          ...props,
          reactStyle: this.getStyle(props.style)
        };
      }
      this.nativeLayer?.setNativeProps(propsToPass);
    }
  }
}
export default AbstractLayer;
//# sourceMappingURL=AbstractLayer.js.map